import { describe, expect, it, vi } from "vitest";
import type { Transport } from "../http/transport.js";
import { getMarkets } from "./markets.js";

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
      },
    ]);
    const transport = { request } as unknown as Transport;

    const markets = await getMarkets(transport);

    expect(request).toHaveBeenCalledWith("GET", "/api/v1/markets/");
    expect(markets[0]?.baseSymbol).toBe("ETH");
    expect(markets[0]?.priceQuantum).toBe(1000n);
  });
});
