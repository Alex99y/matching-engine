// Matches resource: historical trade tape by market. Public (unauthenticated).
//
// Routes (from api/internal/matches/router.go + server.go):
//   GET /api/v1/markets/:market/matches

import type { Transport } from "../http/transport.js";
import type { GetMatchesFilter, Match } from "../types/index.js";
import { parseMatches } from "../utils/parse.js";
import { validateGetMatchesFilter, validateMarket } from "../utils/validation.js";

const MATCHES_BASE = "/api/v1/markets";

/**
 * Fetch recent matches (trades) for a market, newest first — the REST
 * counterpart of {@link streamMarket}'s `"trade"` message, for callers that
 * don't want to hold an SSE connection open.
 *
 * @param transport - SDK transport instance.
 * @param market - Market ref, e.g. `"ETH-USDT"`.
 * @param filter - Optional startDate/endDate (YYYY-MM-DD, end exclusive) and limit (1-100, defaults to 100 server-side).
 * @throws {@link ValidationError} for an empty market, a malformed date, or an out-of-range limit.
 * @throws {@link APIError} (404) for an unknown market.
 * @example
 * const matches = await getMatches(transport, "ETH-USDT", { limit: 20 });
 */
export async function getMatches(
  transport: Transport,
  market: string,
  filter: GetMatchesFilter = {},
): Promise<Match[]> {
  validateMarket(market);
  validateGetMatchesFilter(filter);

  const raw = await transport.request<unknown>("GET", `${MATCHES_BASE}/${market}/matches`, {
    query: {
      ...(filter.startDate !== undefined ? { start_date: filter.startDate } : {}),
      ...(filter.endDate !== undefined ? { end_date: filter.endDate } : {}),
      ...(filter.limit !== undefined ? { limit: filter.limit } : {}),
    },
  });
  return parseMatches(raw);
}
