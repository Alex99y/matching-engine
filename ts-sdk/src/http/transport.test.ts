import { afterEach, describe, expect, it, vi } from "vitest";
import {
  APIError,
  AuthenticationError,
  NetworkError,
  ParseError,
  RateLimitError,
  TimeoutError,
} from "../errors/index.js";
import { Transport, type FetchLike, type TransportConfig } from "./transport.js";

function jsonResponse(
  body: string,
  status = 200,
  headers: Record<string, string> = {},
): Response {
  return new Response(body, {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  });
}

function makeTransport(fetchFn: FetchLike, overrides: Partial<TransportConfig> = {}) {
  const config: TransportConfig = {
    timeoutMs: 1000,
    maxRetries: 0,
    baseRetryDelayMs: 10,
    fetchFn,
    ...overrides,
  };
  return new Transport("http://api.local:8080", config);
}

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("Transport.request — success paths", () => {
  it("returns the parsed body with bigint fields", async () => {
    const fetchFn = vi.fn(async () => jsonResponse('{"price":9007199254740993}'));
    const out = await makeTransport(fetchFn).request<{ price: bigint }>("GET", "/x");
    expect(out.price).toBe(9007199254740993n);
  });

  it("returns undefined for an empty 2xx body", async () => {
    const fetchFn = vi.fn(async () => new Response("", { status: 201 }));
    const out = await makeTransport(fetchFn).request("POST", "/x", { body: { a: 1 } });
    expect(out).toBeUndefined();
  });

  it("sends a bearer token and json body, never leaking the token", async () => {
    const fetchFn = vi.fn(
      (_url: string | URL | Request, _init?: RequestInit) =>
        Promise.resolve(jsonResponse("{}")),
    );
    await makeTransport(fetchFn).request("POST", "/order/", {
      body: { price: 5n },
      token: "secret-token",
    });
    const init = fetchFn.mock.calls[0]?.[1] as RequestInit;
    const headers = init.headers as Record<string, string>;
    expect(headers["Authorization"]).toBe("Bearer secret-token");
    expect(headers["Content-Type"]).toBe("application/json");
    expect(init.body).toBe('{"price":5}');
  });

  it("appends defined query params and skips empty ones", async () => {
    const fetchFn = vi.fn(
      (_url: string | URL | Request, _init?: RequestInit) =>
        Promise.resolve(jsonResponse("[]")),
    );
    await makeTransport(fetchFn).request("GET", "/order/", {
      query: { market: "ETH-USDT", limit: 10, client_order_id: undefined, empty: "" },
    });
    const url = String(fetchFn.mock.calls[0]?.[0]);
    expect(url).toContain("market=ETH-USDT");
    expect(url).toContain("limit=10");
    expect(url).not.toContain("client_order_id");
    expect(url).not.toContain("empty=");
  });
});

describe("Transport.request — error mapping", () => {
  it("maps 404 to APIError with status and server message", async () => {
    const fetchFn = vi.fn(async () =>
      jsonResponse('{"message":"order not found"}', 404),
    );
    const err = await makeTransport(fetchFn)
      .request("GET", "/x")
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(APIError);
    expect((err as APIError).status).toBe(404);
    expect((err as APIError).message).toBe("order not found");
  });

  it("maps 401 to AuthenticationError", async () => {
    const fetchFn = vi.fn(async () => jsonResponse('{"message":"invalid token"}', 401));
    await expect(makeTransport(fetchFn).request("GET", "/x")).rejects.toBeInstanceOf(
      AuthenticationError,
    );
  });

  it("maps 429 to RateLimitError with Retry-After", async () => {
    const fetchFn = vi.fn(async () =>
      jsonResponse('{"message":"slow"}', 429, { "Retry-After": "2" }),
    );
    const err = await makeTransport(fetchFn)
      .request("GET", "/x")
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(RateLimitError);
    expect((err as RateLimitError).retryAfterMs).toBe(2000);
  });

  it("wraps a non-AbortError fetch rejection as NetworkError", async () => {
    const fetchFn = vi.fn(async () => {
      throw new Error("ECONNREFUSED");
    });
    await expect(makeTransport(fetchFn).request("GET", "/x")).rejects.toBeInstanceOf(
      NetworkError,
    );
  });

  it("maps an abort to TimeoutError", async () => {
    vi.useFakeTimers();
    const fetchFn = vi.fn(
      (_url: string | URL | Request, init?: RequestInit) =>
        new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener("abort", () => {
            const e = new Error("aborted");
            e.name = "AbortError";
            reject(e);
          });
        }),
    ) as unknown as FetchLike;
    const promise = makeTransport(fetchFn, { timeoutMs: 50 }).request("GET", "/x");
    const assertion = expect(promise).rejects.toBeInstanceOf(TimeoutError);
    await vi.advanceTimersByTimeAsync(60);
    await assertion;
  });

  it("throws ParseError on malformed 2xx JSON", async () => {
    const fetchFn = vi.fn(async () => jsonResponse("{not json", 200));
    await expect(makeTransport(fetchFn).request("GET", "/x")).rejects.toBeInstanceOf(
      ParseError,
    );
  });

  it("throws ParseError when Content-Length exceeds the cap", async () => {
    const fetchFn = vi.fn(async () =>
      jsonResponse("{}", 200, { "Content-Length": String(6_000_000) }),
    );
    await expect(makeTransport(fetchFn).request("GET", "/x")).rejects.toBeInstanceOf(
      ParseError,
    );
  });
});

describe("Transport.request — retries", () => {
  it("retries 5xx and then succeeds", async () => {
    vi.useFakeTimers();
    let calls = 0;
    const fetchFn = vi.fn(async () => {
      calls += 1;
      return calls === 1 ? jsonResponse("err", 503) : jsonResponse('{"ok":true}');
    });
    const promise = makeTransport(fetchFn, { maxRetries: 2, baseRetryDelayMs: 100 }).request<{
      ok: boolean;
    }>("GET", "/x");
    await vi.advanceTimersByTimeAsync(500);
    await expect(promise).resolves.toEqual({ ok: true });
    expect(calls).toBe(2);
  });

  it("retries network errors up to the limit then throws", async () => {
    vi.useFakeTimers();
    const fetchFn = vi.fn(async () => {
      throw new Error("boom");
    });
    const promise = makeTransport(fetchFn, { maxRetries: 1, baseRetryDelayMs: 50 }).request(
      "GET",
      "/x",
    );
    const assertion = expect(promise).rejects.toBeInstanceOf(NetworkError);
    await vi.advanceTimersByTimeAsync(500);
    await assertion;
    expect(fetchFn).toHaveBeenCalledTimes(2);
  });

  it("does not retry a 4xx response", async () => {
    const fetchFn = vi.fn(async () => jsonResponse('{"message":"bad"}', 400));
    await expect(
      makeTransport(fetchFn, { maxRetries: 3 }).request("GET", "/x"),
    ).rejects.toBeInstanceOf(APIError);
    expect(fetchFn).toHaveBeenCalledTimes(1);
  });
});
