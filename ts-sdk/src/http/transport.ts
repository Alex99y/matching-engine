// HTTP transport: the single boundary where native fetch/abort/parse failures
// are caught and converted into the SDK error hierarchy. Owns timeouts, retries
// (exponential backoff + jitter), and bigint-safe (de)serialization.

import {
  APIError,
  AuthenticationError,
  NetworkError,
  ParseError,
  RateLimitError,
  SDKError,
  TimeoutError,
} from "../errors/index.js";
import { parseWithBigInts, stringifyWithBigInts } from "../utils/json.js";

export interface SSEOptions {
  readonly query?: QueryParams;
  readonly token?: string;
  /** Cancellation signal. Abort it to close the stream. */
  readonly signal?: AbortSignal;
}

export type FetchLike = typeof fetch;

export type QueryValue = string | number | boolean | undefined;
export type QueryParams = Record<string, QueryValue>;

export interface RequestOptions {
  readonly query?: QueryParams;
  readonly body?: unknown;
  /** Bearer token for authenticated routes. Never logged or echoed in errors. */
  readonly token?: string;
}

export interface TransportConfig {
  readonly timeoutMs: number;
  readonly maxRetries: number;
  readonly baseRetryDelayMs: number;
  readonly fetchFn: FetchLike;
}

// Reject responses whose advertised body exceeds this, to bound memory use.
const MAX_BODY_BYTES = 5_000_000;

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function isRetryableStatus(status: number): boolean {
  return status === 429 || status >= 500;
}

function parseRetryAfter(header: string | null): number | undefined {
  if (!header) {
    return undefined;
  }
  const seconds = Number(header);
  if (Number.isFinite(seconds) && seconds >= 0) {
    return seconds * 1000;
  }
  const date = Date.parse(header);
  if (!Number.isNaN(date)) {
    return Math.max(0, date - Date.now());
  }
  return undefined;
}

export class Transport {
  private readonly origin: string;
  private readonly config: TransportConfig;

  constructor(origin: string, config: TransportConfig) {
    this.origin = origin.replace(/\/+$/, "");
    this.config = config;
  }

  /**
   * Perform a request and return the parsed body (or undefined for empty 2xx
   * responses). All failures surface as SDKError subclasses.
   */
  async request<T>(
    method: string,
    path: string,
    options: RequestOptions = {},
  ): Promise<T> {
    const url = this.buildUrl(path, options.query);
    const headers: Record<string, string> = {};
    let body: string | undefined;
    if (options.body !== undefined) {
      headers["Content-Type"] = "application/json";
      body = stringifyWithBigInts(options.body);
    }
    if (options.token) {
      headers["Authorization"] = `Bearer ${options.token}`;
    }

    let attempt = 0;
    // Loop bounded by maxRetries; each branch either returns, retries, or throws.
    for (;;) {
      let response: Response;
      try {
        response = await this.fetchWithTimeout(url, method, headers, body);
      } catch (err) {
        const mapped = this.mapTransportError(err);
        if (attempt < this.config.maxRetries) {
          await this.backoff(attempt);
          attempt += 1;
          continue;
        }
        throw mapped;
      }

      const text = await this.readBody(response);

      if (!response.ok) {
        const apiError = this.mapStatusError(response, text);
        if (
          isRetryableStatus(response.status) &&
          attempt < this.config.maxRetries
        ) {
          const retryAfter =
            apiError instanceof RateLimitError ? apiError.retryAfterMs : undefined;
          await this.backoff(attempt, retryAfter);
          attempt += 1;
          continue;
        }
        throw apiError;
      }

      if (text.length === 0) {
        return undefined as T;
      }
      const contentType = response.headers.get("Content-Type") ?? "";
      if (!contentType.includes("application/json")) {
        return undefined as T;
      }
      try {
        return parseWithBigInts(text) as T;
      } catch (err) {
        throw new ParseError("failed to parse response body", err);
      }
    }
  }

  /**
   * Open an SSE connection and yield raw `data:` payloads as strings. The
   * generator ends when the server closes the stream or the caller aborts it
   * (via `options.signal` or by breaking out of the `for await` loop).
   *
   * All network / status errors are mapped into the SDK error hierarchy before
   * surfacing, matching the behaviour of {@link request}.
   *
   * @throws {@link NetworkError} on connection failure.
   * @throws {@link AuthenticationError} on 401/403.
   * @throws {@link APIError} on any other non-2xx response.
   */
  async *streamSSE(
    path: string,
    options: SSEOptions = {},
  ): AsyncGenerator<string, void, undefined> {
    const url = this.buildUrl(path, options.query);
    const headers: Record<string, string> = {
      Accept: "text/event-stream",
      "Cache-Control": "no-cache",
    };
    if (options.token) {
      headers["Authorization"] = `Bearer ${options.token}`;
    }

    // Internal controller so we can abort on generator cleanup regardless of
    // whether the caller provided their own signal.
    const controller = new AbortController();
    if (options.signal) {
      if (options.signal.aborted) {
        controller.abort();
      } else {
        options.signal.addEventListener("abort", () => { controller.abort(); }, { once: true });
      }
    }

    let response: Response;
    try {
      response = await this.config.fetchFn(url, {
        method: "GET",
        headers,
        signal: controller.signal,
      });
    } catch (err) {
      throw this.mapTransportError(err);
    }

    if (!response.ok) {
      const text = await response.text();
      throw this.mapStatusError(response, text);
    }

    const body = response.body;
    if (!body) {
      throw new NetworkError("SSE response has no body");
    }

    const reader = body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    try {
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });

        // SSE messages are delimited by double newlines.
        const parts = buffer.split("\n\n");
        buffer = parts.pop() ?? "";

        for (const block of parts) {
          let data = "";
          for (const line of block.split("\n")) {
            if (line.startsWith("data: ")) {
              data += (data ? "\n" : "") + line.slice(6);
            }
            // comment (": ping"), event:, id:, retry: lines are all ignored
          }
          if (data) {
            yield data;
          }
        }
      }
    } finally {
      reader.releaseLock();
      controller.abort();
    }
  }

  private async fetchWithTimeout(
    url: string,
    method: string,
    headers: Record<string, string>,
    body: string | undefined,
  ): Promise<Response> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.config.timeoutMs);
    const init: RequestInit = { method, headers, signal: controller.signal };
    if (body !== undefined) {
      init.body = body;
    }
    try {
      return await this.config.fetchFn(url, init);
    } finally {
      clearTimeout(timer);
    }
  }

  private mapTransportError(err: unknown): SDKError {
    if (err instanceof SDKError) {
      return err;
    }
    if (err instanceof Error && err.name === "AbortError") {
      return new TimeoutError(
        `request timed out after ${this.config.timeoutMs}ms`,
        err,
      );
    }
    return new NetworkError("network request failed", err);
  }

  private mapStatusError(response: Response, text: string): APIError {
    const message = extractMessage(text, response.status);
    const body = safeBody(text);
    switch (response.status) {
      case 401:
      case 403:
        return new AuthenticationError(message, response.status, body);
      case 429:
        return new RateLimitError(
          message,
          response.status,
          parseRetryAfter(response.headers.get("Retry-After")),
          body,
        );
      default:
        return new APIError(message, response.status, body);
    }
  }

  private async readBody(response: Response): Promise<string> {
    const declared = Number(response.headers.get("Content-Length"));
    if (Number.isFinite(declared) && declared > MAX_BODY_BYTES) {
      throw new ParseError("response body exceeds maximum allowed size");
    }
    const text = await response.text();
    if (text.length > MAX_BODY_BYTES) {
      throw new ParseError("response body exceeds maximum allowed size");
    }
    return text;
  }

  private async backoff(attempt: number, retryAfterMs?: number): Promise<void> {
    if (retryAfterMs !== undefined) {
      await sleep(retryAfterMs);
      return;
    }
    const base = this.config.baseRetryDelayMs * 2 ** attempt;
    const jitter = Math.random() * this.config.baseRetryDelayMs;
    await sleep(base + jitter);
  }

  private buildUrl(path: string, query?: QueryParams): string {
    const url = new URL(this.origin + path);
    if (query) {
      for (const [key, value] of Object.entries(query)) {
        if (value !== undefined && value !== "") {
          url.searchParams.set(key, String(value));
        }
      }
    }
    return url.toString();
  }
}

function extractMessage(text: string, status: number): string {
  try {
    const parsed: unknown = JSON.parse(text);
    if (
      typeof parsed === "object" &&
      parsed !== null &&
      typeof (parsed as { message?: unknown }).message === "string"
    ) {
      return (parsed as { message: string }).message;
    }
  } catch {
    // Not JSON; fall through to a generic message.
  }
  return `request failed with status ${status}`;
}

function safeBody(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return text.length > 0 ? text : undefined;
  }
}
