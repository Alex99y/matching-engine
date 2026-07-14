import { describe, expect, it, vi } from "vitest";
import type { Transport } from "../http/transport.js";
import { OrderSide, OrderType, TimeInForce } from "../types/index.js";
import { AuthenticatedClient } from "./authenticated-client.js";

const orderRow = {
  id: "o1",
  type: "limit",
  time_in_force: "gtc",
  have_quantity: 1n,
  want_quantity: 2n,
  created_at: 1700000000,
};

function client(result: unknown, token = "tok") {
  const request = vi.fn().mockResolvedValue(result);
  const transport = { request } as unknown as Transport;
  return { session: new AuthenticatedClient(transport, token), request };
}

describe("AuthenticatedClient", () => {
  it("exposes the bearer token", () => {
    const { session } = client(orderRow, "my-token");
    expect(session.authToken).toBe("my-token");
  });

  it("getOrder forwards the token", async () => {
    const { session, request } = client(orderRow);
    const order = await session.getOrder("o1");
    expect(order.id).toBe("o1");
    expect(request.mock.calls[0]?.[2]?.token).toBe("tok");
  });

  it("getOrders returns mapped orders", async () => {
    const { session } = client([orderRow]);
    const orders = await session.getOrders();
    expect(orders).toHaveLength(1);
    expect(orders[0]?.haveQuantity).toBe(1n);
  });

  it("createOrders returns batch results", async () => {
    const { session } = client({ results: [{ index: 0, order_id: "abc" }] });
    const resp = await session.createOrders([
      {
        market: "ETH-USDT",
        side: OrderSide.Buy,
        type: OrderType.Limit,
        timeInForce: TimeInForce.GoodTillCancel,
        price: 1n,
        quantity: 1n,
      },
    ]);
    expect(resp.results[0]?.orderId).toBe("abc");
  });

  it("getBalances returns parsed balances and forwards the token", async () => {
    const { session, request } = client([
      { name: "Ether", symbol: "ETH", decimals: 18, balance: 3n, blocked: 1n },
    ]);
    const balances = await session.getBalances();
    expect(balances).toHaveLength(1);
    expect(balances[0]?.symbol).toBe("ETH");
    expect(balances[0]?.balance).toBe(3n);
    expect(request.mock.calls[0]?.[2]?.token).toBe("tok");
  });

  it("cancelOrders sends DELETE with order_ids body and forwards the token", async () => {
    const { session, request } = client({ results: [{ order_id: "o1" }] });
    await session.cancelOrders(["o1"]);
    expect(request).toHaveBeenCalledWith("DELETE", "/api/v1/order/", {
      token: "tok",
      body: { order_ids: ["o1"] },
    });
  });

  it("logout sends DELETE /api/v1/sessions and forwards the token", async () => {
    const { session, request } = client(undefined);
    await session.logout();
    expect(request).toHaveBeenCalledWith("DELETE", "/api/v1/sessions", { token: "tok" });
  });

  it("keeps two sessions independent", () => {
    const a = client(orderRow, "token-a").session;
    const b = client(orderRow, "token-b").session;
    expect(a.authToken).toBe("token-a");
    expect(b.authToken).toBe("token-b");
  });
});
