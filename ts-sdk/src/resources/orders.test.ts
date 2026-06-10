import { describe, expect, it, vi } from "vitest";
import { ValidationError } from "../errors/index.js";
import type { Transport } from "../http/transport.js";
import { OrderSide, OrderType, TimeInForce } from "../types/index.js";
import { cancelOrder, createOrder, getOrder, getOrders } from "./orders.js";

const TOKEN = "tok";

function stubTransport(result: unknown) {
  const request = vi.fn().mockResolvedValue(result);
  return { transport: { request } as unknown as Transport, request };
}

const orderRow = {
  id: "o1",
  type: "limit",
  time_in_force: "gtc",
  have_quantity: 100n,
  want_quantity: 200n,
  created_at: 1700000000,
};

describe("orders.cancelOrder", () => {
  it("sends DELETE to the order endpoint with the bearer token", async () => {
    const { transport, request } = stubTransport(undefined);
    await cancelOrder(transport, TOKEN, "o1");
    expect(request).toHaveBeenCalledWith("DELETE", "/api/v1/order/o1", { token: TOKEN });
  });

  it("rejects an empty orderId without calling the API", async () => {
    const { transport, request } = stubTransport(undefined);
    await expect(cancelOrder(transport, TOKEN, "")).rejects.toBeInstanceOf(ValidationError);
    expect(request).not.toHaveBeenCalled();
  });

  it("URL-encodes the orderId", async () => {
    const { transport, request } = stubTransport(undefined);
    await cancelOrder(transport, TOKEN, "a/b");
    expect(request).toHaveBeenCalledWith("DELETE", "/api/v1/order/a%2Fb", { token: TOKEN });
  });
});

describe("orders.getOrder", () => {
  it("requests a single order with the token", async () => {
    const { transport, request } = stubTransport(orderRow);
    const order = await getOrder(transport, TOKEN, "o1");
    expect(request).toHaveBeenCalledWith("GET", "/api/v1/order/o1", { token: TOKEN });
    expect(order.id).toBe("o1");
  });

  it("rejects an empty id without calling the API", async () => {
    const { transport, request } = stubTransport(orderRow);
    await expect(getOrder(transport, TOKEN, "")).rejects.toBeInstanceOf(ValidationError);
    expect(request).not.toHaveBeenCalled();
  });
});

describe("orders.getOrders", () => {
  it("builds the query, sending show flags only when enabled", async () => {
    const { transport, request } = stubTransport([orderRow]);
    await getOrders(transport, TOKEN, {
      market: "ETH-USDT",
      limit: 25,
      showOpen: true,
    });
    expect(request).toHaveBeenCalledWith("GET", "/api/v1/order/", {
      token: TOKEN,
      query: {
        client_order_id: undefined,
        market: "ETH-USDT",
        start_date: undefined,
        end_date: undefined,
        limit: 25,
        show_open: "true",
      },
    });
  });

  it("omits show flags when not requested", async () => {
    const { transport, request } = stubTransport([]);
    await getOrders(transport, TOKEN, {});
    const query = request.mock.calls[0]?.[2]?.query as Record<string, unknown>;
    expect(query).not.toHaveProperty("show_open");
    expect(query).not.toHaveProperty("show_cancelled");
  });

  it("rejects an invalid limit", async () => {
    const { transport, request } = stubTransport([]);
    await expect(getOrders(transport, TOKEN, { limit: 999 })).rejects.toBeInstanceOf(
      ValidationError,
    );
    expect(request).not.toHaveBeenCalled();
  });
});

describe("orders.createOrder", () => {
  it("serializes the order body with bigint amounts", async () => {
    const { transport, request } = stubTransport({ order_id: "new-id" });
    const result = await createOrder(transport, TOKEN, {
      market: "ETH-USDT",
      side: OrderSide.Buy,
      type: OrderType.Limit,
      timeInForce: TimeInForce.GoodTillCancel,
      price: 2000n,
      quantity: 5n,
    });
    expect(request).toHaveBeenCalledWith("POST", "/api/v1/order/", {
      token: TOKEN,
      body: {
        order_side: "buy",
        order_type: "limit",
        order_tif: "gtc",
        market: "ETH-USDT",
        price: 2000n,
        quantity: 5n,
      },
    });
    expect(result.orderId).toBe("new-id");
  });

  it("includes optional fields only when provided", async () => {
    const { transport, request } = stubTransport({ order_id: "x" });
    await createOrder(transport, TOKEN, {
      market: "ETH-USDT",
      side: OrderSide.Sell,
      type: OrderType.Market,
      timeInForce: TimeInForce.ImmediateOrCancel,
      clientOrderId: "c".repeat(32),
      quoteQty: 9n,
      expiresAt: 1700000900,
    });
    const body = request.mock.calls[0]?.[2]?.body as Record<string, unknown>;
    expect(body["client_order_id"]).toBe("c".repeat(32));
    expect(body["quote_qty"]).toBe(9n);
    expect(body["expires_at"]).toBe(1700000900);
    expect(body).not.toHaveProperty("price");
  });

  it("rejects an invalid side before calling the API", async () => {
    const { transport, request } = stubTransport({ order_id: "x" });
    await expect(
      createOrder(transport, TOKEN, {
        market: "ETH-USDT",
        side: "diagonal" as OrderSide,
        type: OrderType.Limit,
        timeInForce: TimeInForce.GoodTillCancel,
      }),
    ).rejects.toBeInstanceOf(ValidationError);
    expect(request).not.toHaveBeenCalled();
  });
});
