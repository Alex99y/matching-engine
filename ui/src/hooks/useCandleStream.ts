import { useEffect, useState } from "react";
import type { MatchingEngineClient } from "ts-sdk";
import { CANDLE_HISTORY_BARS } from "../config.ts";

// ── Types ─────────────────────────────────────────────────────────────────

// Named OHLCBar (not Bar) to avoid collision with lightweight-charts' own Bar type.
export interface OHLCBar {
  time: number;
  open: number;
  high: number;
  low: number;
  close: number;
}

export interface CandleStreamState {
  /** Closed historical bars — set once per market/interval change. */
  bars: OHLCBar[];
  /** The currently forming (incomplete) bar — updates on every live trade. */
  formingBar: OHLCBar | null;
  status: "loading" | "live" | "error";
  error: string | null;
}

// ── Helper ────────────────────────────────────────────────────────────────

function toNum(n: bigint): number {
  return Number(n);
}

// ── Hook ──────────────────────────────────────────────────────────────────

export function useCandleStream(
  client: MatchingEngineClient,
  market: string,
  interval: number,
): CandleStreamState {
  const [state, setState] = useState<CandleStreamState>({
    bars: [],
    formingBar: null,
    status: "loading",
    error: null,
  });

  useEffect(() => {
    if (!market || !interval) return;

    setState({ bars: [], formingBar: null, status: "loading", error: null });

    const ac = new AbortController();
    let active = true;

    // Mutable forming bar. Updated per-trade and flushed into React state.
    // Stored in an object wrapper so TypeScript can narrow the inner field
    // instead of trying to narrow the mutable let directly.
    const ref: { bar: OHLCBar | null } = { bar: null };

    (async () => {
      try {
        // 1. Fetch historical bars.
        const now = Math.floor(Date.now() / 1000);
        const from = now - interval * CANDLE_HISTORY_BARS;
        const { candles } = await client.getCandles(market, {
          interval: interval as Parameters<typeof client.getCandles>[1]["interval"],
          from,
          to: now,
        });

        if (!active) return;

        const historicalBars: OHLCBar[] = candles.map((c) => ({
          time: c.bucketStart,
          open: toNum(c.open),
          high: toNum(c.high),
          low: toNum(c.low),
          close: toNum(c.close),
        }));

        setState((prev) => ({ ...prev, bars: historicalBars }));

        // 2. Subscribe to live SSE stream.
        for await (const msg of client.streamCandles(
          market,
          interval as Parameters<typeof client.streamCandles>[1],
          { signal: ac.signal },
        )) {
          if (!active) break;

          switch (msg.type) {
            case "candle.snapshot": {
              const bar: OHLCBar = {
                time: msg.bucketStart,
                open: toNum(msg.open),
                high: toNum(msg.high),
                low: toNum(msg.low),
                close: toNum(msg.close),
              };
              ref.bar = bar;
              setState((prev) => ({ ...prev, formingBar: bar, status: "live" }));
              break;
            }

            case "candle.trade": {
              const prev = ref.bar;
              if (!prev) break;
              const price = toNum(msg.price);
              const updated: OHLCBar = {
                time: prev.time,
                open: prev.open,
                high: prev.high < price ? price : prev.high,
                low: prev.low > price ? price : prev.low,
                close: price,
              };
              ref.bar = updated;
              setState((s) => ({ ...s, formingBar: updated }));
              break;
            }

            case "candle.closed": {
              const closed = ref.bar;
              ref.bar = null;
              if (closed) {
                setState((prev) => ({
                  ...prev,
                  bars: [...prev.bars, closed],
                  formingBar: null,
                }));
              }
              break;
            }
          }
        }
      } catch (err) {
        if (active && !ac.signal.aborted) {
          setState((prev) => ({
            ...prev,
            status: "error",
            error: err instanceof Error ? err.message : String(err),
          }));
        }
      }
    })();

    return () => {
      active = false;
      ac.abort();
    };
  }, [client, market, interval]);

  return state;
}
