import { describe, expect, it } from "vitest";
import { ParseError } from "../errors/index.js";
import {
  parseBatchCancelOrderResponse,
  parseBatchCreateOrderResponse,
  parseInstruments,
  parseLoginToken,
  parseMarkets,
  parseOrder,
  parseOrders,
  parseStreamMessage,
} from "./parse.js";

describe("parseMarkets", () => {
  it("maps wire fields to camelCase with bigint amounts", () => {
    const markets = parseMarkets([
      {
        base_symbol: "ETH",
        quote_symbol: "USDT",
        price_quantum: 1000n,
        amount_quantum: 100n,
        min_order_size: 1n,
        max_order_size: 1000000n,
      },
    ]);
    expect(markets[0]).toEqual({
      baseSymbol: "ETH",
      quoteSymbol: "USDT",
      priceQuantum: 1000n,
      amountQuantum: 100n,
      minOrderSize: 1n,
      maxOrderSize: 1000000n,
    });
  });

  it("accepts number for bigint fields as a defensive fallback", () => {
    const markets = parseMarkets([
      {
        base_symbol: "ETH",
        quote_symbol: "USDT",
        price_quantum: 1000,
        amount_quantum: 100,
        min_order_size: 1,
        max_order_size: 1000,
      },
    ]);
    expect(markets[0]?.priceQuantum).toBe(1000n);
  });

  it("throws ParseError when not an array", () => {
    expect(() => parseMarkets({})).toThrow(ParseError);
  });

  it("throws ParseError on a missing required field", () => {
    expect(() => parseMarkets([{ base_symbol: "ETH" }])).toThrow(ParseError);
  });

  it("throws ParseError when a bigint field is a non-integer", () => {
    expect(() =>
      parseMarkets([
        {
          base_symbol: "ETH",
          quote_symbol: "USDT",
          price_quantum: 1.5,
          amount_quantum: 1n,
          min_order_size: 1n,
          max_order_size: 1n,
        },
      ]),
    ).toThrow(ParseError);
  });
});

describe("parseInstruments", () => {
  it("maps instrument fields", () => {
    const out = parseInstruments([
      { name: "Ether", symbol: "ETH", decimals: 18, created_at: "2026-01-01T00:00:00Z" },
    ]);
    expect(out[0]).toEqual({
      name: "Ether",
      symbol: "ETH",
      decimals: 18,
      createdAt: "2026-01-01T00:00:00Z",
    });
  });

  it("throws ParseError when decimals is not a number", () => {
    expect(() =>
      parseInstruments([{ name: "x", symbol: "x", decimals: "8", created_at: "t" }]),
    ).toThrow(ParseError);
  });
});

describe("parseOrder", () => {
  const base = {
    id: "o1",
    type: "limit",
    time_in_force: "gtc",
    have_quantity: 100n,
    want_quantity: 200n,
    created_at: 1700000000,
  };

  it("parses a minimal order", () => {
    const order = parseOrder(base);
    expect(order.id).toBe("o1");
    expect(order.haveQuantity).toBe(100n);
    expect(order.clientOrderId).toBeUndefined();
    expect(order.openOrder).toBeUndefined();
  });

  it("parses optional client order id and open order", () => {
    const order = parseOrder({
      ...base,
      client_order_id: "cid",
      expires_at: 1700000900,
      open_order: { price: 50n, side: "buy", remaining_have: 10n, remaining_want: 20n },
    });
    expect(order.clientOrderId).toBe("cid");
    expect(order.expiresAt).toBe(1700000900);
    expect(order.openOrder).toEqual({
      price: 50n,
      side: "buy",
      remainingHave: 10n,
      remainingWant: 20n,
    });
  });

  it("parses a cancelled order", () => {
    const order = parseOrder({
      ...base,
      cancelled_order: { cancelled_at: 1700000500, remaining_have: 5n, remaining_want: 6n },
    });
    expect(order.cancelledOrder).toEqual({
      cancelledAt: 1700000500,
      remainingHave: 5n,
      remainingWant: 6n,
    });
  });

  it("throws ParseError when id is missing", () => {
    const { id, ...rest } = base;
    void id;
    expect(() => parseOrder(rest)).toThrow(ParseError);
  });

  it("parseOrders throws ParseError when not an array", () => {
    expect(() => parseOrders("nope")).toThrow(ParseError);
  });
});

describe("parseBatchCreateOrderResponse", () => {
  it("maps successful and failed items", () => {
    const resp = parseBatchCreateOrderResponse({
      results: [
        { index: 0, order_id: "abc" },
        { index: 1, error: "market not found" },
      ],
    });
    expect(resp.results[0]).toEqual({ index: 0, orderId: "abc" });
    expect(resp.results[1]).toEqual({ index: 1, error: "market not found" });
  });

  it("throws ParseError when results is missing", () => {
    expect(() => parseBatchCreateOrderResponse({})).toThrow(ParseError);
  });

  it("throws ParseError when index is missing from an item", () => {
    expect(() =>
      parseBatchCreateOrderResponse({ results: [{ order_id: "x" }] }),
    ).toThrow(ParseError);
  });
});

describe("parseBatchCancelOrderResponse", () => {
  it("maps successful and failed items", () => {
    const resp = parseBatchCancelOrderResponse({
      results: [
        { order_id: "o1" },
        { order_id: "o2", error: "order not found" },
      ],
    });
    expect(resp.results[0]).toEqual({ orderId: "o1" });
    expect(resp.results[1]).toEqual({ orderId: "o2", error: "order not found" });
  });

  it("throws ParseError when order_id is missing from an item", () => {
    expect(() =>
      parseBatchCancelOrderResponse({ results: [{ error: "bad" }] }),
    ).toThrow(ParseError);
  });
});

describe("parseLoginToken", () => {
  it("extracts the token", () => {
    expect(parseLoginToken({ token: "jwt" })).toBe("jwt");
  });

  it("throws ParseError when token is missing", () => {
    expect(() => parseLoginToken({})).toThrow(ParseError);
  });
});

// ---- parseStreamMessage ----

describe("parseStreamMessage", () => {
  it("parses a heartbeat message", () => {
    expect(parseStreamMessage(JSON.stringify({ type: "heartbeat" }))).toEqual({
      type: "heartbeat",
    });
  });

  it("parses a snapshot with bigint price and quantity from strings", () => {
    const raw = JSON.stringify({
      type: "snapshot",
      market: "ETH-USDT",
      bids: [{ price: "2000", quantity: "10" }],
      asks: [{ price: "2001", quantity: "5" }],
    });
    expect(parseStreamMessage(raw)).toEqual({
      type: "snapshot",
      market: "ETH-USDT",
      bids: [{ price: 2000n, quantity: 10n }],
      asks: [{ price: 2001n, quantity: 5n }],
    });
  });

  it("parses a book delta — preserves uint64 precision above 2^53", () => {
    const price = "9007199254740993"; // 2^53 + 1, unrepresentable as number
    const raw = JSON.stringify({ type: "book", side: "sell", price, quantity: "0" });
    const msg = parseStreamMessage(raw);
    expect(msg).toEqual({ type: "book", side: "sell", price: 9007199254740993n, quantity: 0n });
  });

  it("parses a trade message", () => {
    const raw = JSON.stringify({
      type: "trade",
      price: "3000",
      quantity: "7",
      taker_side: "buy",
    });
    expect(parseStreamMessage(raw)).toEqual({
      type: "trade",
      price: 3000n,
      quantity: 7n,
      takerSide: "buy",
    });
  });

  it("parses an order message with status and amounts", () => {
    const raw = JSON.stringify({
      type: "order",
      order_id: "order-1",
      status: "partially_filled",
      filled: "3",
      remaining: "7",
    });
    expect(parseStreamMessage(raw)).toEqual({
      type: "order",
      orderId: "order-1",
      status: "partially_filled",
      filled: 3n,
      remaining: 7n,
    });
  });

  it("throws ParseError on malformed JSON", () => {
    expect(() => parseStreamMessage("{bad json")).toThrow(ParseError);
  });

  it("throws ParseError on an unknown type discriminant", () => {
    expect(() => parseStreamMessage(JSON.stringify({ type: "unknown" }))).toThrow(ParseError);
  });

  it("throws ParseError when a required amount field is missing", () => {
    expect(() =>
      parseStreamMessage(JSON.stringify({ type: "book", side: "buy", price: "100" })),
    ).toThrow(ParseError);
  });

  it("throws ParseError when an amount is not a valid integer string", () => {
    expect(() =>
      parseStreamMessage(JSON.stringify({ type: "book", side: "buy", price: "abc", quantity: "1" })),
    ).toThrow(ParseError);
  });

  it("throws ParseError when side is not buy or sell", () => {
    expect(() =>
      parseStreamMessage(JSON.stringify({ type: "book", side: "invalid", price: "1", quantity: "1" })),
    ).toThrow(ParseError);
  });
});
