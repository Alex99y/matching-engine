import { describe, expect, it, vi } from "vitest";
import { ValidationError } from "../errors/index.js";
import type { Transport } from "../http/transport.js";
import { OrderSide, OrderType, TimeInForce } from "../types/index.js";
import { cancelOrders, createOrders, getOrder, getOrders } from "./orders.js";

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

// ---- cancelOrders ----

describe("orders.cancelOrders", () => {
  it("sends DELETE to the base endpoint with order_ids body", async () => {
    const { transport, request } = stubTransport({ results: [] });
    await cancelOrders(transport, TOKEN, ["o1", "o2"]);
    expect(request).toHaveBeenCalledWith("DELETE", "/api/v1/order/", {
      token: TOKEN,
      body: { order_ids: ["o1", "o2"] },
    });
  });

  it("returns per-item results", async () => {
    const { transport } = stubTransport({
      results: [
        { order_id: "o1" },
        { order_id: "o2", error: "order not found" },
      ],
    });
    const resp = await cancelOrders(transport, TOKEN, ["o1", "o2"]);
    expect(resp.results).toHaveLength(2);
    expect(resp.results[0]?.orderId).toBe("o1");
    expect(resp.results[0]?.error).toBeUndefined();
    expect(resp.results[1]?.orderId).toBe("o2");
    expect(resp.results[1]?.error).toBe("order not found");
  });

  it("rejects an empty array without calling the API", async () => {
    const { transport, request } = stubTransport({ results: [] });
    await expect(cancelOrders(transport, TOKEN, [])).rejects.toBeInstanceOf(ValidationError);
    expect(request).not.toHaveBeenCalled();
  });

  it("rejects a batch larger than 500", async () => {
    const { transport, request } = stubTransport({ results: [] });
    await expect(
      cancelOrders(transport, TOKEN, Array.from({ length: 501 }, () => "id")),
    ).rejects.toBeInstanceOf(ValidationError);
    expect(request).not.toHaveBeenCalled();
  });

  it("rejects an empty string in the array", async () => {
    const { transport, request } = stubTransport({ results: [] });
    await expect(cancelOrders(transport, TOKEN, ["o1", ""])).rejects.toBeInstanceOf(
      ValidationError,
    );
    expect(request).not.toHaveBeenCalled();
  });
});

// ---- createOrders ----

describe("orders.createOrders", () => {
  it("serializes each order into the array body", async () => {
    const { transport, request } = stubTransport({
      results: [{ index: 0, order_id: "new-id" }],
    });
    const resp = await createOrders(transport, TOKEN, [
      {
        market: "ETH-USDT",
        side: OrderSide.Buy,
        type: OrderType.Limit,
        timeInForce: TimeInForce.GoodTillCancel,
        price: 2000n,
        quantity: 5n,
      },
    ]);
    expect(request).toHaveBeenCalledWith("POST", "/api/v1/order/", {
      token: TOKEN,
      body: [
        {
          order_side: "buy",
          order_type: "limit",
          order_tif: "gtc",
          market: "ETH-USDT",
          price: 2000n,
          quantity: 5n,
        },
      ],
    });
    expect(resp.results[0]?.orderId).toBe("new-id");
    expect(resp.results[0]?.index).toBe(0);
  });

  it("includes optional fields only when provided", async () => {
    const { transport, request } = stubTransport({
      results: [{ index: 0, order_id: "x" }],
    });
    await createOrders(transport, TOKEN, [
      {
        market: "ETH-USDT",
        side: OrderSide.Sell,
        type: OrderType.Market,
        timeInForce: TimeInForce.ImmediateOrCancel,
        clientOrderId: "c".repeat(32),
        quoteQty: 9n,
        expiresAt: 1700000900,
      },
    ]);
    const body = request.mock.calls[0]?.[2]?.body as Record<string, unknown>[];
    expect(body[0]?.["client_order_id"]).toBe("c".repeat(32));
    expect(body[0]?.["quote_qty"]).toBe(9n);
    expect(body[0]?.["expires_at"]).toBe(1700000900);
    expect(body[0]).not.toHaveProperty("price");
  });

  it("returns per-item error for failed items", async () => {
    const { transport } = stubTransport({
      results: [
        { index: 0, order_id: "ok-id" },
        { index: 1, error: "market not found" },
      ],
    });
    const resp = await createOrders(transport, TOKEN, [
      {
        market: "ETH-USDT",
        side: OrderSide.Buy,
        type: OrderType.Limit,
        timeInForce: TimeInForce.GoodTillCancel,
      },
      {
        market: "UNKNOWN",
        side: OrderSide.Sell,
        type: OrderType.Limit,
        timeInForce: TimeInForce.GoodTillCancel,
      },
    ]);
    expect(resp.results[0]?.orderId).toBe("ok-id");
    expect(resp.results[0]?.error).toBeUndefined();
    expect(resp.results[1]?.error).toBe("market not found");
    expect(resp.results[1]?.orderId).toBeUndefined();
  });

  it("rejects an empty array without calling the API", async () => {
    const { transport, request } = stubTransport({ results: [] });
    await expect(createOrders(transport, TOKEN, [])).rejects.toBeInstanceOf(ValidationError);
    expect(request).not.toHaveBeenCalled();
  });

  it("rejects a batch larger than 500", async () => {
    const { transport, request } = stubTransport({ results: [] });
    await expect(
      createOrders(
        transport,
        TOKEN,
        Array.from({ length: 501 }, () => ({
          market: "ETH-USDT",
          side: OrderSide.Buy,
          type: OrderType.Limit,
          timeInForce: TimeInForce.GoodTillCancel,
        })),
      ),
    ).rejects.toBeInstanceOf(ValidationError);
    expect(request).not.toHaveBeenCalled();
  });

  it("rejects an invalid side on any item before calling the API", async () => {
    const { transport, request } = stubTransport({ results: [] });
    await expect(
      createOrders(transport, TOKEN, [
        {
          market: "ETH-USDT",
          side: "diagonal" as OrderSide,
          type: OrderType.Limit,
          timeInForce: TimeInForce.GoodTillCancel,
        },
      ]),
    ).rejects.toBeInstanceOf(ValidationError);
    expect(request).not.toHaveBeenCalled();
  });
});

// ---- getOrder ----

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

// ---- getOrders ----

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
