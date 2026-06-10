import { createServer, type IncomingMessage, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  APIError,
  MatchingEngineClient,
  OrderSide,
  OrderType,
  TimeInForce,
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

      // Private routes require the bearer token issued at login.
      if (url.pathname.startsWith("/api/v1/order") && auth !== "Bearer e2e-token") {
        sendJson(401, '{"message":"missing or invalid authorization header"}');
        return;
      }

      switch (route) {
        case "POST /api/v1/users/register":
          res.writeHead(201).end();
          return;
        case "POST /api/v1/users/login":
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
          sendJson(202, '{"order_id":"order-1"}');
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

    const { orderId } = await session.createOrder({
      market: "ETH-USDT",
      side: OrderSide.Buy,
      type: OrderType.Limit,
      timeInForce: TimeInForce.GoodTillCancel,
      price: PRICE,
      quantity: 5n,
    });
    expect(orderId).toBe("order-1");

    // The server received the bigint price as an exact, unquoted integer.
    expect(lastCreateOrderRaw).toContain(`"price":${PRICE.toString()}`);
    expect(lastCreateOrderRaw).toContain('"order_side":"buy"');

    const orders = await session.getOrders({ market: "ETH-USDT", showOpen: true });
    expect(orders[0]?.wantQuantity).toBe(10000000000000000000n);
    expect(orders[0]?.openOrder?.price).toBe(2000n);

    const order = await session.getOrder(orderId);
    expect(order.id).toBe("order-1");

    await expect(session.logout()).resolves.toBeUndefined();
  });

  it("surfaces a not-found order as an APIError(404)", async () => {
    const session = await client.login({ username: "bot", password: "supersecret" });
    const err = await session.getOrder("does-not-exist").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(APIError);
    expect((err as APIError).status).toBe(404);
  });
});
