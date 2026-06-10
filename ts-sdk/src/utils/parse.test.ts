import { describe, expect, it } from "vitest";
import { ParseError } from "../errors/index.js";
import {
  parseCreateOrderResult,
  parseInstruments,
  parseLoginToken,
  parseMarkets,
  parseOrder,
  parseOrders,
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

describe("parseCreateOrderResult", () => {
  it("maps order_id", () => {
    expect(parseCreateOrderResult({ order_id: "abc" })).toEqual({ orderId: "abc" });
  });

  it("throws ParseError when order_id is missing", () => {
    expect(() => parseCreateOrderResult({})).toThrow(ParseError);
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
