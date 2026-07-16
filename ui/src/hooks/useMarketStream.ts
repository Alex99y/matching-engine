import { useEffect, useReducer } from "react";
import type { MatchingEngineClient, BookLevel } from "ts-sdk";
import { BOOK_DEPTH } from "../config.ts";

// ── Types ─────────────────────────────────────────────────────────────────

export interface BookState {
  bids: BookLevel[]; // sorted desc by price, capped to BOOK_DEPTH
  asks: BookLevel[]; // sorted asc by price, capped to BOOK_DEPTH
  lastTradePrice: bigint | null;
  lastTradeSide: "buy" | "sell" | null;
  status: "connecting" | "live" | "error";
  error: string | null;
}

type BookAction =
  | { type: "connecting" }
  | { type: "snapshot"; bids: readonly BookLevel[]; asks: readonly BookLevel[] }
  | { type: "delta"; side: "buy" | "sell"; price: bigint; quantity: bigint }
  | { type: "trade"; price: bigint; takerSide: "buy" | "sell" }
  | { type: "error"; message: string };

// ── Reducer ───────────────────────────────────────────────────────────────

function applyDelta(
  levels: BookLevel[],
  price: bigint,
  quantity: bigint,
  descending: boolean,
): BookLevel[] {
  const filtered = levels.filter((l) => l.price !== price);
  if (quantity === 0n) return filtered;
  const next = [...filtered, { price, quantity }];
  next.sort((a, b) =>
    descending
      ? a.price > b.price ? -1 : a.price < b.price ? 1 : 0
      : a.price < b.price ? -1 : a.price > b.price ? 1 : 0,
  );
  return next;
}

const INITIAL: BookState = {
  bids: [],
  asks: [],
  lastTradePrice: null,
  lastTradeSide: null,
  status: "connecting",
  error: null,
};

function reducer(state: BookState, action: BookAction): BookState {
  switch (action.type) {
    case "connecting":
      return { ...INITIAL };

    case "snapshot": {
      const bids = [...action.bids].sort((a, b) =>
        a.price > b.price ? -1 : a.price < b.price ? 1 : 0,
      );
      const asks = [...action.asks].sort((a, b) =>
        a.price < b.price ? -1 : a.price > b.price ? 1 : 0,
      );
      return {
        ...state,
        bids: bids.slice(0, BOOK_DEPTH),
        asks: asks.slice(0, BOOK_DEPTH),
        status: "live",
        error: null,
      };
    }

    case "delta": {
      if (action.side === "buy") {
        const bids = applyDelta(state.bids, action.price, action.quantity, true);
        return { ...state, bids: bids.slice(0, BOOK_DEPTH) };
      } else {
        const asks = applyDelta(state.asks, action.price, action.quantity, false);
        return { ...state, asks: asks.slice(0, BOOK_DEPTH) };
      }
    }

    case "trade":
      return {
        ...state,
        lastTradePrice: action.price,
        lastTradeSide: action.takerSide,
      };

    case "error":
      return { ...state, status: "error", error: action.message };
  }
}

// ── Hook ──────────────────────────────────────────────────────────────────

export function useMarketStream(
  client: MatchingEngineClient,
  market: string,
): BookState {
  const [state, dispatch] = useReducer(reducer, INITIAL);

  useEffect(() => {
    if (!market) return;

    dispatch({ type: "connecting" });

    const ac = new AbortController();
    let active = true;

    (async () => {
      try {
        for await (const msg of client.streamMarket(market, { signal: ac.signal })) {
          if (!active) break;
          switch (msg.type) {
            case "snapshot":
              dispatch({ type: "snapshot", bids: msg.bids, asks: msg.asks });
              break;
            case "book":
              dispatch({
                type: "delta",
                side: msg.side,
                price: msg.price,
                quantity: msg.quantity,
              });
              break;
            case "trade":
              dispatch({
                type: "trade",
                price: msg.price,
                takerSide: msg.takerSide,
              });
              break;
            case "heartbeat":
              break;
          }
        }
      } catch (err) {
        if (active && !ac.signal.aborted) {
          dispatch({
            type: "error",
            message: err instanceof Error ? err.message : String(err),
          });
        }
      }
    })();

    return () => {
      active = false;
      ac.abort();
    };
  }, [client, market]);

  return state;
}
