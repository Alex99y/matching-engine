import { useEffect, useState } from "react";
import type { MarketPrice, MatchingEngineClient } from "ts-sdk";
import { PRICE_TICKER_POLL_MS } from "../config.ts";

// Polls GET /markets/prices on an interval — there's no SSE feed for
// all-market tickers (only per-market streamMarket), so this can't be
// pushed. Public endpoint — works for guests too. Fails silently on a
// missed tick (same as useInstruments): a stale price for 10s is a much
// better failure mode than toasting on every dropped poll.
export function useMarketPrices(client: MatchingEngineClient | null): MarketPrice[] {
  const [prices, setPrices] = useState<MarketPrice[]>([]);

  useEffect(() => {
    if (!client) {
      setPrices([]);
      return;
    }
    const c = client;
    let active = true;

    function load() {
      c.getPrices().then((list) => {
        if (active) setPrices(list);
      }).catch(() => {});
    }

    load();
    const id = setInterval(load, PRICE_TICKER_POLL_MS);
    return () => { active = false; clearInterval(id); };
  }, [client]);

  return prices;
}
