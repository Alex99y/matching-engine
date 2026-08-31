import { describe, expect, it, vi } from "vitest";
import { ValidationError } from "../errors/index.js";
import type { Transport } from "../http/transport.js";
import { getDepth, getMarkets, getPrices } from "./markets.js";

describe("markets.getMarkets", () => {
  it("requests the markets endpoint and maps the response", async () => {
    const request = vi.fn().mockResolvedValue([
      {
        base_symbol: "ETH",
        quote_symbol: "USDT",
        price_quantum: 1000n,
        amount_quantum: 100n,
        min_order_size: 1n,
        max_order_size: 5n,
        taker_fee_bps: 100n,
        maker_fee_bps: 50n,
      },
    ]);
    const transport = { request } as unknown as Transport;

    const markets = await getMarkets(transport);

    expect(request).toHaveBeenCalledWith("GET", "/api/v1/markets/");
    expect(markets[0]?.baseSymbol).toBe("ETH");
    expect(markets[0]?.priceQuantum).toBe(1000n);
  });
});

describe("markets.getPrices", () => {
  it("requests the prices endpoint and maps a full row", async () => {
    const request = vi.fn().mockResolvedValue([
      {
        market: "BTC-USDT",
        // "price" is a BIGINT_WIRE_FIELDS key, so transport.request has
        // already revived it as bigint by the time the resource sees it.
        price: 11000n,
        min_price_24h: "9000",
        max_price_24h: "12000",
        volume_24h: "5",
        change_percent_24h: "10.00",
      },
    ]);
    const transport = { request } as unknown as Transport;

    const prices = await getPrices(transport);

    expect(request).toHaveBeenCalledWith("GET", "/api/v1/markets/prices");
    expect(prices).toEqual([
      {
        market: "BTC-USDT",
        price: 11000n,
        minPrice24h: 9000n,
        maxPrice24h: 12000n,
        volume24h: 5n,
        changePercent24h: "10.00",
      },
    ]);
  });

  it("omits stat fields for a market with no matches", async () => {
    const request = vi.fn().mockResolvedValue([{ market: "ETH-USDT" }]);
    const transport = { request } as unknown as Transport;

    const prices = await getPrices(transport);

    expect(prices).toEqual([{ market: "ETH-USDT" }]);
  });
});

describe("markets.getDepth", () => {
  it("requests the depth endpoint and maps the response", async () => {
    const request = vi.fn().mockResolvedValue({
      market: "ETH-USDT",
      bids: [{ price: 100n, quantity: 2n }],
      asks: [{ price: 101n, quantity: 4n }],
    });
    const transport = { request } as unknown as Transport;

    const depth = await getDepth(transport, "ETH-USDT");

    expect(request).toHaveBeenCalledWith(
      "GET",
      "/api/v1/markets/ETH-USDT/depth",
      expect.objectContaining({}),
    );
    expect(depth).toEqual({
      market: "ETH-USDT",
      bids: [{ price: 100n, quantity: 2n }],
      asks: [{ price: 101n, quantity: 4n }],
    });
  });

  it("passes group as a query param when given", async () => {
    const request = vi.fn().mockResolvedValue({ market: "ETH-USDT", bids: [], asks: [] });
    const transport = { request } as unknown as Transport;

    await getDepth(transport, "ETH-USDT", { group: 100n });

    expect(request).toHaveBeenCalledWith(
      "GET",
      "/api/v1/markets/ETH-USDT/depth",
      expect.objectContaining({ query: { group: "100" } }),
    );
  });

  it("rejects an empty market", async () => {
    const transport = { request: vi.fn() } as unknown as Transport;
    await expect(getDepth(transport, "")).rejects.toBeInstanceOf(ValidationError);
  });

  it("rejects a non-positive group", async () => {
    const transport = { request: vi.fn() } as unknown as Transport;
    await expect(getDepth(transport, "ETH-USDT", { group: 0n })).rejects.toBeInstanceOf(
      ValidationError,
    );
  });
});
