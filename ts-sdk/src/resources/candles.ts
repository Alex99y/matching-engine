// Candles resource: OHLCV historical data. Public (unauthenticated).
//
// Routes (from api/internal/candles/router.go + server.go):
//   GET /api/v1/markets/:market/candles

import type { Transport } from "../http/transport.js";
import { CandleInterval, type GetCandlesParams, type GetCandlesResponse } from "../types/index.js";
import { parseGetCandlesResponse } from "../utils/parse.js";
import { validateGetCandlesParams } from "../utils/validation.js";

const CANDLES_BASE = "/api/v1/markets";

/**
 * Fetch historical OHLCV candles for a market.
 *
 * Intervals are in seconds — use the {@link CandleInterval} constants for
 * readability (`CandleInterval.OneMinute`, `CandleInterval.OneHour`, etc.).
 * The range `[from, to)` must span at most 1000 candles.
 *
 * @param transport - SDK transport instance.
 * @param market - Market ref, e.g. `"ETH-USDT"`.
 * @param params - Interval, from and to as unix seconds.
 * @throws {@link ValidationError} for an empty market, invalid interval, bad timestamps, or a range exceeding 1000 candles.
 * @throws {@link APIError} (404) for an unknown market.
 * @example
 * const now = Math.floor(Date.now() / 1000);
 * const { candles } = await getCandles(transport, "ETH-USDT", {
 *   interval: CandleInterval.OneMinute,
 *   from: now - 3600,
 *   to: now,
 * });
 */
export async function getCandles(
  transport: Transport,
  market: string,
  params: GetCandlesParams,
): Promise<GetCandlesResponse> {
  validateGetCandlesParams(market, params);
  const raw = await transport.request<unknown>("GET", `${CANDLES_BASE}/${market}/candles`, {
    query: {
      interval: params.interval,
      from: params.from,
      to: params.to,
    },
  });
  return parseGetCandlesResponse(raw);
}

export { CandleInterval };
