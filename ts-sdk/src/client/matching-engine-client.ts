import { ValidationError } from "../errors/index.js";
import {
  Transport,
  type FetchLike,
  type TransportConfig,
} from "../http/transport.js";
import * as instrumentsResource from "../resources/instruments.js";
import * as marketsResource from "../resources/markets.js";
import * as usersResource from "../resources/users.js";
import type {
  Instrument,
  LoginParams,
  Market,
  RegisterParams,
} from "../types/index.js";
import { AuthenticatedClient } from "./authenticated-client.js";

export interface ClientOptions {
  /**
   * Allow plain http instead of https. DANGEROUS — intended only for local
   * development against a non-TLS API. Defaults to false (https required).
   */
  readonly allowInsecure?: boolean;
  /** Per-request timeout in milliseconds. Defaults to 30000. */
  readonly timeoutMs?: number;
  /** Max retry attempts for 429/5xx and network errors. Defaults to 2. */
  readonly maxRetries?: number;
  /** Base delay for exponential backoff between retries. Defaults to 200ms. */
  readonly baseRetryDelayMs?: number;
  /** Override the fetch implementation (testing / custom environments). */
  readonly fetch?: FetchLike;
}

const DEFAULT_TIMEOUT_MS = 30_000;
const DEFAULT_MAX_RETRIES = 2;
const DEFAULT_BASE_RETRY_DELAY_MS = 200;

/**
 * Public entry point of the matching-engine SDK. Construct it with the API
 * host and port, then call {@link login} to obtain an {@link AuthenticatedClient}
 * for the private (order) endpoints. Stateless and free of singletons: multiple
 * clients with different configs coexist without interfering.
 *
 * @example
 * const client = new MatchingEngineClient("api.exchange.com", 443);
 * await client.register({ username, email, password });
 * const session = await client.login({ username, password });
 * const markets = await client.getMarkets();
 */
export class MatchingEngineClient {
  private readonly transport: Transport;

  /**
   * @param host - API host name (no scheme).
   * @param port - API port.
   * @param options - Transport/security options (see {@link ClientOptions}).
   * @throws {@link ValidationError} for an empty host, invalid port, or an
   * http target without `allowInsecure`, or when no fetch implementation exists.
   */
  constructor(host: string, port: number, options: ClientOptions = {}) {
    if (!host) {
      throw new ValidationError("host is required");
    }
    if (!Number.isInteger(port) || port <= 0 || port > 65535) {
      throw new ValidationError("port must be an integer between 1 and 65535");
    }

    const fetchFn = options.fetch ?? globalThis.fetch;
    if (typeof fetchFn !== "function") {
      throw new ValidationError(
        "global fetch is not available; pass options.fetch explicitly",
      );
    }

    const scheme = options.allowInsecure ? "http" : "https";
    const config: TransportConfig = {
      timeoutMs: options.timeoutMs ?? DEFAULT_TIMEOUT_MS,
      maxRetries: options.maxRetries ?? DEFAULT_MAX_RETRIES,
      baseRetryDelayMs: options.baseRetryDelayMs ?? DEFAULT_BASE_RETRY_DELAY_MS,
      fetchFn,
    };
    this.transport = new Transport(`${scheme}://${host}:${port}`, config);
  }

  /**
   * Register a new user. Returns nothing on success (HTTP 201).
   *
   * @throws {@link ValidationError} when any field is empty.
   * @throws {@link APIError} (409) when the username is already taken.
   * @example
   * await client.register({ username: "bot1", email: "bot@x.io", password: "s3cr3t-pass" });
   */
  async register(params: RegisterParams): Promise<void> {
    return usersResource.register(this.transport, params);
  }

  /**
   * Authenticate and obtain a session client for private endpoints.
   *
   * @throws {@link ValidationError} when username or password is empty.
   * @throws {@link AuthenticationError} (401) on bad credentials.
   * @example
   * const session = await client.login({ username: "bot1", password: "s3cr3t-pass" });
   */
  async login(params: LoginParams): Promise<AuthenticatedClient> {
    const token = await usersResource.login(this.transport, params);
    return new AuthenticatedClient(this.transport, token);
  }

  /**
   * List all markets (trading pairs).
   *
   * @throws {@link APIError} on server-side failures.
   * @example
   * const markets = await client.getMarkets();
   */
  async getMarkets(): Promise<Market[]> {
    return marketsResource.getMarkets(this.transport);
  }

  /**
   * List all instruments (tradable assets).
   *
   * @throws {@link APIError} on server-side failures.
   * @example
   * const instruments = await client.getInstruments();
   */
  async getInstruments(): Promise<Instrument[]> {
    return instrumentsResource.getInstruments(this.transport);
  }
}
