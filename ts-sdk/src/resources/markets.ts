// Markets resource: list available trading pairs. Public (unauthenticated).

import type { Transport } from "../http/transport.js";
import type { Market } from "../types/index.js";
import { parseMarkets } from "../utils/parse.js";

const MARKETS_PATH = "/api/v1/markets/";

export async function getMarkets(transport: Transport): Promise<Market[]> {
  const raw = await transport.request<unknown>("GET", MARKETS_PATH);
  return parseMarkets(raw);
}
