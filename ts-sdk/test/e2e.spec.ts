import { createServer, type IncomingMessage, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  APIError,
  AuthenticationError,
  MatchingEngineClient,
  OrderSide,
  OrderType,
  SessionScope,
  TimeInForce,
  type StreamMessage,
} from "../src/index.js";

// Captures the raw POST /order/ body so we can assert the bigint price arrived
// as an exact, unquoted JSON integer (precision preserved end to end).
let lastCreateOrderRaw = "";

const PRICE = 9007199254740993n; // 2^53 + 1: unrepresentable as a JS number.

function readBody(req: IncomingMessage): Promise<string> {
  return new Promise((resolve) => {
    let data = "";
    req.on("data", (chunk) => {
      data += chunk;
    });
    req.on("end", () => resolve(data));
  });
}

function startServer(): Promise<Server> {
  const server = createServer((req, res) => {
    void (async () => {
      const url = new URL(req.url ?? "/", "http://localhost");
      const route = `${req.method} ${url.pathname}`;
      const auth = req.headers["authorization"];
      const sendJson = (status: number, payload: string) => {
        res.writeHead(status, { "Content-Type": "application/json" });
        res.end(payload);
      };

      // Any of these three tokens authenticates: "e2e-token" is the login (full/write)
      // session; "read-tok"/"write-tok" simulate tokens minted via createToken, scoped
      // as their name says. Only "e2e-token" is login-origin.
      const bearer = auth?.startsWith("Bearer ") ? auth.slice("Bearer ".length) : undefined;
      const tokenScope: Record<string, "write" | "read"> = {
        "e2e-token": "write",
        "write-tok": "write",
        "read-tok": "read",
      };
      const isLoginOrigin = bearer === "e2e-token";

      // Private routes require one of the tokens above.
      const isPrivate =
        url.pathname.startsWith("/api/v1/order") ||
        url.pathname === "/api/v1/users/balances" ||
        url.pathname === "/api/v1/stream/users" ||
        url.pathname === "/api/v1/sessions/active" ||
        url.pathname === "/api/v1/sessions/refresh" ||
        url.pathname === "/api/v1/sessions/tokens";
      if (isPrivate && !(bearer && bearer in tokenScope)) {
        sendJson(401, '{"message":"missing or invalid authorization header"}');
        return;
      }
      // Trading routes require a write-scoped token.
      if (
        (route === "POST /api/v1/order/" || route === "DELETE /api/v1/order/") &&
        bearer &&
        tokenScope[bearer] !== "write"
      ) {
        sendJson(403, '{"message":"this action requires a write-scoped session"}');
        return;
      }
      // Minting and revoking another session require the login-origin token.
      if (
        (route === "POST /api/v1/sessions/tokens" || route === "DELETE /api/v1/sessions/active") &&
        !isLoginOrigin
      ) {
        sendJson(403, '{"message":"this action requires a login session"}');
        return;
      }

      // SSE routes: handled before the switch because the path contains a
      // variable market segment that can't be matched with a string literal.
      if (req.method === "GET" && url.pathname.startsWith("/api/v1/stream/")) {
        const segment = url.pathname.slice("/api/v1/stream/".length);
        res.writeHead(200, {
          "Content-Type": "text/event-stream",
          "Cache-Control": "no-cache",
        });
        if (segment === "users") {
          res.write(
            'data: {"type":"order","order_id":"o1","status":"filled","filled":"5","remaining":"0"}\n\n',
          );
        } else {
          // public market stream: one heartbeat then done
          res.write('data: {"type":"heartbeat"}\n\n');
        }
        res.end();
        return;
      }

      switch (route) {
        case "POST /api/v1/users/register":
          res.writeHead(201).end();
          return;
        case "POST /api/v1/sessions":
          sendJson(200, '{"token":"e2e-token"}');
          return;
        case "GET /api/v1/markets/":
          sendJson(
            200,
            '[{"base_symbol":"ETH","quote_symbol":"USDT","price_quantum":1000000000000000000,"amount_quantum":1000,"min_order_size":1,"max_order_size":1000000}]',
          );
          return;
        case "GET /api/v1/instruments/":
          sendJson(
            200,
            '[{"name":"Ether","symbol":"ETH","decimals":18,"created_at":"2026-01-01T00:00:00Z"}]',
          );
          return;
        case "POST /api/v1/order/":
          lastCreateOrderRaw = await readBody(req);
          sendJson(202, '{"results":[{"index":0,"order_id":"order-1"}]}');
          return;
        case "GET /api/v1/order/":
          sendJson(
            200,
            '[{"id":"order-1","type":"limit","time_in_force":"gtc","have_quantity":5,"want_quantity":10000000000000000000,"created_at":1700000000,"open_order":{"price":2000,"side":"buy","remaining_have":5,"remaining_want":10}}]',
          );
          return;
        case "GET /api/v1/order/order-1":
          sendJson(
            200,
            '{"id":"order-1","type":"limit","time_in_force":"gtc","have_quantity":5,"want_quantity":10,"created_at":1700000000}',
          );
          return;
        case "GET /api/v1/users/balances":
          sendJson(
            200,
            '[{"name":"Ether","symbol":"ETH","decimals":18,"balance":5000000000000000000,"blocked":1000000000000000000}]',
          );
          return;
        case "DELETE /api/v1/sessions":
          res.writeHead(204).end();
          return;
        case "GET /api/v1/sessions/active":
          sendJson(
            200,
            '[{"session_id":"hash-1","created_at":1700000000,"expires_at":1700604800,"origin":"login","scope":"write"}]',
          );
          return;
        case "DELETE /api/v1/sessions/active":
          sendJson(200, "null");
          return;
        case "POST /api/v1/sessions/refresh":
          sendJson(200, '{"expires_at":1700604800}');
          return;
        case "POST /api/v1/sessions/tokens": {
          const body = JSON.parse(await readBody(req)) as { scope: "read" | "write" };
          sendJson(
            201,
            JSON.stringify({ token: `${body.scope}-tok`, scope: body.scope, expires_at: 1700604800 }),
          );
          return;
        }
        default:
          sendJson(404, '{"message":"not found"}');
      }
    })();
  });
  return new Promise((resolve) => server.listen(0, "127.0.0.1", () => resolve(server)));
}

describe("end-to-end flow against a mock server", () => {
  let server: Server;
  let client: MatchingEngineClient;

  beforeAll(async () => {
    server = await startServer();
    const { port } = server.address() as AddressInfo;
    client = new MatchingEngineClient("127.0.0.1", port, {
      allowInsecure: true,
      maxRetries: 0,
    });
  });

  afterAll(() => {
    server.close();
  });

  it("runs register -> public reads -> login -> order lifecycle", async () => {
    await expect(
      client.register({ username: "bot", email: "bot@x.io", password: "supersecret" }),
    ).resolves.toBeUndefined();

    const markets = await client.getMarkets();
    expect(markets[0]?.priceQuantum).toBe(1000000000000000000n);

    const instruments = await client.getInstruments();
    expect(instruments[0]?.symbol).toBe("ETH");

    const session = await client.login({ username: "bot", password: "supersecret" });
    expect(session.authToken).toBe("e2e-token");

    const { results: createResults } = await session.createOrders([{
      market: "ETH-USDT",
      side: OrderSide.Buy,
      type: OrderType.Limit,
      timeInForce: TimeInForce.GoodTillCancel,
      price: PRICE,
      quantity: 5n,
    }]);
    const orderId = createResults[0]?.orderId ?? "";
    expect(orderId).toBe("order-1");

    // The server received the bigint price as an exact, unquoted integer.
    expect(lastCreateOrderRaw).toContain(`"price":${PRICE.toString()}`);
    expect(lastCreateOrderRaw).toContain('"order_side":"buy"');

    const orders = await session.getOrders({ market: "ETH-USDT", showOpen: true });
    expect(orders[0]?.wantQuantity).toBe(10000000000000000000n);
    expect(orders[0]?.openOrder?.price).toBe(2000n);

    const order = await session.getOrder(orderId);
    expect(order.id).toBe("order-1");

    const balances = await session.getBalances();
    expect(balances[0]?.symbol).toBe("ETH");
    expect(balances[0]?.balance).toBe(5000000000000000000n);
    expect(balances[0]?.blocked).toBe(1000000000000000000n);

    await expect(session.logout()).resolves.toBeUndefined();
  });

  it("lists and revokes active sessions", async () => {
    const session = await client.login({ username: "bot", password: "supersecret" });

    const active = await session.getActiveSessions();
    expect(active).toEqual([
      {
        sessionId: "hash-1",
        createdAt: 1700000000,
        expiresAt: 1700604800,
        origin: "login",
        scope: "write",
      },
    ]);

    await expect(session.revokeSession(active[0]!.sessionId)).resolves.toBeUndefined();
  });

  it("refreshes a session", async () => {
    const session = await client.login({ username: "bot", password: "supersecret" });
    await expect(session.refreshSession()).resolves.toEqual({ expiresAt: 1700604800 });
  });

  it("mints a read-scoped token and rejects trading with it, but a write-scoped one works", async () => {
    const session = await client.login({ username: "bot", password: "supersecret" });

    const readToken = await session.createToken(SessionScope.Read);
    expect(readToken).toEqual({ token: "read-tok", scope: "read", expiresAt: 1700604800 });

    const readSession = client.withToken(readToken.token);
    const rejected = await readSession
      .createOrders([
        {
          market: "ETH-USDT",
          side: OrderSide.Buy,
          type: OrderType.Limit,
          timeInForce: TimeInForce.GoodTillCancel,
          price: 1n,
          quantity: 1n,
        },
      ])
      .catch((e: unknown) => e);
    expect(rejected).toBeInstanceOf(AuthenticationError);
    expect((rejected as AuthenticationError).status).toBe(403);

    const writeToken = await session.createToken(SessionScope.Write);
    const writeSession = client.withToken(writeToken.token);
    const { results } = await writeSession.createOrders([
      {
        market: "ETH-USDT",
        side: OrderSide.Buy,
        type: OrderType.Limit,
        timeInForce: TimeInForce.GoodTillCancel,
        price: 1n,
        quantity: 1n,
      },
    ]);
    expect(results[0]?.orderId).toBe("order-1");
  });

  it("a minted token cannot mint another token", async () => {
    const session = await client.login({ username: "bot", password: "supersecret" });
    const { token } = await session.createToken(SessionScope.Write);
    const minted = client.withToken(token);
    const rejected = await minted.createToken(SessionScope.Read).catch((e: unknown) => e);
    expect(rejected).toBeInstanceOf(AuthenticationError);
    expect((rejected as AuthenticationError).status).toBe(403);
  });

  it("surfaces a not-found order as an APIError(404)", async () => {
    const session = await client.login({ username: "bot", password: "supersecret" });
    const err = await session.getOrder("does-not-exist").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(APIError);
    expect((err as APIError).status).toBe(404);
  });

  it("streams public market heartbeat via streamMarket", async () => {
    const msgs: StreamMessage[] = [];
    for await (const msg of client.streamMarket("ETH-USDT")) {
      msgs.push(msg);
    }
    expect(msgs).toHaveLength(1);
    expect(msgs[0]).toEqual({ type: "heartbeat" });
  });

  it("streams private order event via streamUser", async () => {
    const session = await client.login({ username: "bot", password: "supersecret" });
    const msgs: StreamMessage[] = [];
    for await (const msg of session.streamUser()) {
      msgs.push(msg);
    }
    expect(msgs).toHaveLength(1);
    expect(msgs[0]).toMatchObject({
      type: "order",
      orderId: "o1",
      status: "filled",
      filled: 5n,
      remaining: 0n,
    });
  });

  it("rejects streamUser with an invalid token as AuthenticationError", async () => {
    // Forge an invalid token by constructing the client manually — skip login
    // so the mock server returns 401 on the stream/users endpoint.
    const { AuthenticatedClient } = await import("../src/client/authenticated-client.js");
    const { Transport } = await import("../src/http/transport.js");
    const { port } = server.address() as import("node:net").AddressInfo;
    const transport = new Transport(`http://127.0.0.1:${port}`, {
      timeoutMs: 5000,
      maxRetries: 0,
      baseRetryDelayMs: 0,
      fetchFn: fetch,
    });
    const badClient = new AuthenticatedClient(transport, "bad-token");
    const err = await (async () => {
      for await (const _ of badClient.streamUser()) {
        // should not reach here
      }
    })().catch((e: unknown) => e);
    const { AuthenticationError } = await import("../src/errors/index.js");
    expect(err).toBeInstanceOf(AuthenticationError);
  });
});
