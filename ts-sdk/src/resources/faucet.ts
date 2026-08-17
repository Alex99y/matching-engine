// Faucet resource: sandbox/POC self-serve funding. Authenticated, write-scoped.
//
// Routes (from api/internal/faucet/router.go + server.go):
//   POST /api/v1/faucet

import type { Transport } from "../http/transport.js";
import type { FaucetResult } from "../types/index.js";
import { parseFaucetResult } from "../utils/parse.js";
import { validateInstrumentSymbol } from "../utils/validation.js";

const FAUCET_PATH = "/api/v1/faucet";

/**
 * Credit the authenticated user with a small, fixed amount of an instrument, for
 * testing. The amount is hardcoded per-instrument server-side (see
 * `faucetAmountTenths` in api/internal/faucet/service.go) — the caller cannot
 * choose it. Sandbox/POC only — the API applies no rate limit and no lifetime
 * cap, so this can be called repeatedly to accumulate balance.
 *
 * @param transport - SDK transport instance.
 * @param token - Bearer token for a write-scoped session.
 * @param instrument - Instrument symbol, e.g. "BTC".
 * @throws {@link ValidationError} when `instrument` is empty.
 * @throws {@link AuthenticationError} (403) when called from a read-scoped session.
 * @throws {@link APIError} (404) when the instrument does not exist.
 */
export async function requestFaucetFunds(
  transport: Transport,
  token: string,
  instrument: string,
): Promise<FaucetResult> {
  validateInstrumentSymbol(instrument);
  const raw = await transport.request<unknown>("POST", FAUCET_PATH, {
    query: { instrument },
    token,
  });
  return parseFaucetResult(raw);
}
