import { describe, expect, it, vi } from "vitest";
import { ParseError, ValidationError } from "../errors/index.js";
import type { Transport } from "../http/transport.js";
import type { StreamMessage } from "../types/index.js";
import { streamMarket, streamUser } from "./stream.js";

const TOKEN = "tok";

// Stub an async generator from a fixed list of raw data strings.
function makeGen(...items: string[]): AsyncGenerator<string, void, undefined> {
  return (async function* () {
    for (const item of items) {
      yield item;
    }
  })();
}

function stubTransport(...data: string[]) {
  const streamSSE = vi.fn().mockReturnValue(makeGen(...data));
  return { transport: { streamSSE } as unknown as Transport, streamSSE };
}

async function collect(gen: AsyncGenerator<StreamMessage>): Promise<StreamMessage[]> {
  const msgs: StreamMessage[] = [];
  for await (const msg of gen) {
    msgs.push(msg);
  }
  return msgs;
}

// ---- streamMarket ----

describe("streamMarket", () => {
  it("calls streamSSE with the correct path for the given market", async () => {
    const { transport, streamSSE } = stubTransport();
    await collect(streamMarket(transport, "ETH-USDT"));
    expect(streamSSE).toHaveBeenCalledWith(
      "/api/v1/stream/ETH-USDT",
      expect.objectContaining({}),
    );
  });

  it("yields parsed heartbeat messages", async () => {
    const { transport } = stubTransport(JSON.stringify({ type: "heartbeat" }));
    const msgs = await collect(streamMarket(transport, "ETH-USDT"));
    expect(msgs).toHaveLength(1);
    expect(msgs[0]).toEqual({ type: "heartbeat" });
  });

  it("yields a parsed snapshot with bigint amounts", async () => {
    const payload = JSON.stringify({
      type: "snapshot",
      market: "ETH-USDT",
      bids: [{ price: "2000", quantity: "10" }],
      asks: [{ price: "2001", quantity: "5" }],
    });
    const { transport } = stubTransport(payload);
    const msgs = await collect(streamMarket(transport, "ETH-USDT"));
    expect(msgs[0]).toEqual({
      type: "snapshot",
      market: "ETH-USDT",
      bids: [{ price: 2000n, quantity: 10n }],
      asks: [{ price: 2001n, quantity: 5n }],
    });
  });

  it("yields a parsed book delta", async () => {
    const payload = JSON.stringify({
      type: "book",
      side: "buy",
      price: "9007199254740993",
      quantity: "0",
    });
    const { transport } = stubTransport(payload);
    const msgs = await collect(streamMarket(transport, "ETH-USDT"));
    expect(msgs[0]).toEqual({
      type: "book",
      side: "buy",
      price: 9007199254740993n,
      quantity: 0n,
    });
  });

  it("yields a parsed trade", async () => {
    const payload = JSON.stringify({
      type: "trade",
      price: "3000",
      quantity: "7",
      taker_side: "sell",
    });
    const { transport } = stubTransport(payload);
    const msgs = await collect(streamMarket(transport, "ETH-USDT"));
    expect(msgs[0]).toEqual({ type: "trade", price: 3000n, quantity: 7n, takerSide: "sell" });
  });

  it("passes group as a string query param", async () => {
    const { transport, streamSSE } = stubTransport();
    await collect(streamMarket(transport, "ETH-USDT", { group: 100n }));
    expect(streamSSE).toHaveBeenCalledWith(
      "/api/v1/stream/ETH-USDT",
      expect.objectContaining({ query: { group: "100" } }),
    );
  });

  it("passes the abort signal through to streamSSE", async () => {
    const { transport, streamSSE } = stubTransport();
    const controller = new AbortController();
    await collect(streamMarket(transport, "ETH-USDT", { signal: controller.signal }));
    expect(streamSSE).toHaveBeenCalledWith(
      "/api/v1/stream/ETH-USDT",
      expect.objectContaining({ signal: controller.signal }),
    );
  });

  it("throws ValidationError for an empty market string", async () => {
    const { transport } = stubTransport();
    await expect(collect(streamMarket(transport, ""))).rejects.toBeInstanceOf(ValidationError);
  });

  it("throws ValidationError for a zero group value", async () => {
    const { transport } = stubTransport();
    await expect(
      collect(streamMarket(transport, "ETH-USDT", { group: 0n })),
    ).rejects.toBeInstanceOf(ValidationError);
  });

  it("throws ValidationError for a negative group value", async () => {
    const { transport } = stubTransport();
    await expect(
      collect(streamMarket(transport, "ETH-USDT", { group: -1n })),
    ).rejects.toBeInstanceOf(ValidationError);
  });

  it("surfaces a ParseError from an unknown message type", async () => {
    const { transport } = stubTransport(JSON.stringify({ type: "unknown_type" }));
    await expect(collect(streamMarket(transport, "ETH-USDT"))).rejects.toBeInstanceOf(ParseError);
  });
});

// ---- streamUser ----

describe("streamUser", () => {
  it("calls streamSSE with the /users path and the bearer token", async () => {
    const { transport, streamSSE } = stubTransport();
    await collect(streamUser(transport, TOKEN));
    expect(streamSSE).toHaveBeenCalledWith(
      "/api/v1/stream/users",
      expect.objectContaining({ token: TOKEN }),
    );
  });

  it("yields a parsed order message", async () => {
    const payload = JSON.stringify({
      type: "order",
      order_id: "order-1",
      status: "filled",
      filled: "5",
      remaining: "0",
    });
    const { transport } = stubTransport(payload);
    const msgs = await collect(streamUser(transport, TOKEN));
    expect(msgs[0]).toEqual({
      type: "order",
      orderId: "order-1",
      status: "filled",
      filled: 5n,
      remaining: 0n,
    });
  });

  it("passes the abort signal through to streamSSE", async () => {
    const { transport, streamSSE } = stubTransport();
    const controller = new AbortController();
    await collect(streamUser(transport, TOKEN, { signal: controller.signal }));
    expect(streamSSE).toHaveBeenCalledWith(
      "/api/v1/stream/users",
      expect.objectContaining({ signal: controller.signal }),
    );
  });

  it("yields multiple messages in order", async () => {
    const frames = [
      JSON.stringify({ type: "order", order_id: "o1", status: "open", filled: "0", remaining: "10" }),
      JSON.stringify({ type: "order", order_id: "o1", status: "filled", filled: "10", remaining: "0" }),
    ];
    const { transport } = stubTransport(...frames);
    const msgs = await collect(streamUser(transport, TOKEN));
    expect(msgs).toHaveLength(2);
    expect((msgs[0] as { status: string }).status).toBe("open");
    expect((msgs[1] as { status: string }).status).toBe("filled");
  });
});
