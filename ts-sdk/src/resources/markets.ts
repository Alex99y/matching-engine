// Markets resource: list available trading pairs, and one-shot order-book
// depth snapshots. Public (unauthenticated).
//
// Routes (from api/internal/markets/router.go + server.go):
//   GET /api/v1/markets/
//   GET /api/v1/markets/:market/depth

import type { Transport } from "../http/transport.js";
import type { GetDepthOptions, Market, MarketDepth } from "../types/index.js";
import { parseMarketDepth, parseMarkets } from "../utils/parse.js";
import { validateGetDepthOptions, validateMarket } from "../utils/validation.js";

const MARKETS_PATH = "/api/v1/markets/";
const MARKETS_BASE = "/api/v1/markets";

export async function getMarkets(transport: Transport): Promise<Market[]> {
  const raw = await transport.request<unknown>("GET", MARKETS_PATH);
  return parseMarkets(raw);
}

/**
 * Fetch a one-shot order-book depth snapshot for one market — the REST
 * counterpart of {@link streamMarket}'s first frame, for callers that don't
 * want to hold an SSE connection open (polling clients, bots).
 *
 * @param transport - SDK transport instance.
 * @param market - Market ref, e.g. `"ETH-USDT"`.
 * @param options - Optional price-bucket grouping.
 * @throws {@link ValidationError} for an empty market or a non-positive group.
 * @throws {@link APIError} (404) for an unknown market, (400) for an invalid group.
 */
export async function getDepth(
  transport: Transport,
  market: string,
  options: GetDepthOptions = {},
): Promise<MarketDepth> {
  validateMarket(market);
  validateGetDepthOptions(options);

  const raw = await transport.request<unknown>("GET", `${MARKETS_BASE}/${market}/depth`, {
    ...(options.group !== undefined ? { query: { group: options.group.toString() } } : {}),
  });
  return parseMarketDepth(raw);
}
