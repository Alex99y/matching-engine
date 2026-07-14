import { describe, expect, it, vi } from "vitest";
import { AuthenticationError, ValidationError } from "../errors/index.js";
import type { FetchLike } from "../http/transport.js";
import { AuthenticatedClient } from "./authenticated-client.js";
import { MatchingEngineClient } from "./matching-engine-client.js";

function jsonResponse(body: string, status = 200): Response {
  return new Response(body, {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

// Routes requests by "METHOD path" so a single client exercises the full surface.
function routerFetch(
  routes: Record<string, () => Response>,
): { fetchFn: FetchLike; calls: string[] } {
  const calls: string[] = [];
  const fetchFn = vi.fn(
    (input: string | URL | Request, init?: RequestInit): Promise<Response> => {
      const url = new URL(String(input));
      const key = `${init?.method ?? "GET"} ${url.pathname}`;
      calls.push(key);
      const handler = routes[key];
      if (!handler) {
        return Promise.resolve(jsonResponse('{"message":"no route"}', 404));
      }
      return Promise.resolve(handler());
    },
  ) as unknown as FetchLike;
  return { fetchFn, calls };
}

function makeClient(fetchFn: FetchLike) {
  return new MatchingEngineClient("api.local", 8080, {
    allowInsecure: true,
    maxRetries: 0,
    fetch: fetchFn,
  });
}

describe("MatchingEngineClient constructor", () => {
  it("throws on empty host", () => {
    expect(() => new MatchingEngineClient("", 80, { fetch: globalThis.fetch })).toThrow(
      ValidationError,
    );
  });

  it("throws on invalid port", () => {
    expect(
      () => new MatchingEngineClient("h", 0, { fetch: globalThis.fetch }),
    ).toThrow(ValidationError);
    expect(
      () => new MatchingEngineClient("h", 70000, { fetch: globalThis.fetch }),
    ).toThrow(ValidationError);
  });

  it("throws when no fetch implementation is available", () => {
    vi.stubGlobal("fetch", undefined);
    try {
      expect(() => new MatchingEngineClient("h", 80)).toThrow(ValidationError);
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("defaults to https and only uses http with allowInsecure", async () => {
    const secure = routerFetch({ "GET /api/v1/markets/": () => jsonResponse("[]") });
    const client = new MatchingEngineClient("api.local", 443, { fetch: secure.fetchFn });
    await client.getMarkets();
    // Capture the URL the transport built.
    const url = String((secure.fetchFn as unknown as { mock: { calls: unknown[][] } }).mock.calls[0]?.[0]);
    expect(url.startsWith("https://")).toBe(true);
  });
});

describe("MatchingEngineClient public methods", () => {
  it("register posts to the users endpoint", async () => {
    const { fetchFn, calls } = routerFetch({
      "POST /api/v1/users/register": () => new Response("", { status: 201 }),
    });
    await makeClient(fetchFn).register({ username: "u", email: "e@x.io", password: "pw" });
    expect(calls).toContain("POST /api/v1/users/register");
  });

  it("login returns an AuthenticatedClient carrying the token", async () => {
    const { fetchFn } = routerFetch({
      "POST /api/v1/sessions": () => jsonResponse('{"token":"jwt-xyz"}'),
    });
    const session = await makeClient(fetchFn).login({ username: "u", password: "pw" });
    expect(session).toBeInstanceOf(AuthenticatedClient);
    expect(session.authToken).toBe("jwt-xyz");
  });

  it("login surfaces a 401 as AuthenticationError", async () => {
    const { fetchFn } = routerFetch({
      "POST /api/v1/sessions": () =>
        jsonResponse('{"message":"invalid username or password"}', 401),
    });
    await expect(
      makeClient(fetchFn).login({ username: "u", password: "pw" }),
    ).rejects.toBeInstanceOf(AuthenticationError);
  });

  it("getMarkets and getInstruments map responses", async () => {
    const { fetchFn } = routerFetch({
      "GET /api/v1/markets/": () =>
        jsonResponse(
          '[{"base_symbol":"ETH","quote_symbol":"USDT","price_quantum":1000,"amount_quantum":100,"min_order_size":1,"max_order_size":5}]',
        ),
      "GET /api/v1/instruments/": () =>
        jsonResponse('[{"name":"Ether","symbol":"ETH","decimals":18,"created_at":"t"}]'),
    });
    const client = makeClient(fetchFn);
    const markets = await client.getMarkets();
    const instruments = await client.getInstruments();
    expect(markets[0]?.priceQuantum).toBe(1000n);
    expect(instruments[0]?.symbol).toBe("ETH");
  });
});
