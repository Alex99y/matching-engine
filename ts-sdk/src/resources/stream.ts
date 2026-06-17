// Stream resource: SSE endpoints for live market data and private order updates.
//
// Routes (from api/internal/stream/router.go + server.go):
//   GET /api/v1/stream/:market  — public book + trade + heartbeat stream
//   GET /api/v1/stream/users    — private per-user order update stream (auth required)

import type { SSEOptions, Transport } from "../http/transport.js";
import type { MarketStreamOptions, StreamMessage, UserStreamOptions } from "../types/index.js";
import { parseStreamMessage } from "../utils/parse.js";
import { validateMarket, validateMarketStreamOptions } from "../utils/validation.js";

const STREAM_BASE = "/api/v1/stream";

/**
 * Open a public SSE stream for one market, yielding book, trade, and
 * heartbeat events. The first frame is always a full book snapshot.
 *
 * The generator ends when the server closes the connection (reconnect to
 * re-subscribe) or when `options.signal` is aborted.
 *
 * @param transport - SDK transport instance.
 * @param market - Market ref, e.g. `"ETH-USDT"`.
 * @param options - Optional grouping and cancellation signal.
 * @throws {@link ValidationError} for an empty market or a non-positive group.
 * @throws {@link APIError} (404) for an unknown market.
 * @throws {@link NetworkError} on connection failure.
 */
export async function* streamMarket(
  transport: Transport,
  market: string,
  options: MarketStreamOptions = {},
): AsyncGenerator<StreamMessage, void, undefined> {
  validateMarket(market);
  validateMarketStreamOptions(options);

  const sseOpts: SSEOptions = {
    ...(options.group !== undefined ? { query: { group: options.group.toString() } } : {}),
    ...(options.signal !== undefined ? { signal: options.signal } : {}),
  };

  for await (const data of transport.streamSSE(`${STREAM_BASE}/${market}`, sseOpts)) {
    yield parseStreamMessage(data);
  }
}

/**
 * Open a private SSE stream for the authenticated user, yielding order
 * lifecycle events (`OrderMessage`). Requires a valid bearer token.
 *
 * The generator ends when the server closes the connection or when
 * `options.signal` is aborted.
 *
 * @param transport - SDK transport instance.
 * @param token - Bearer token from {@link AuthenticatedClient}.
 * @param options - Optional cancellation signal.
 * @throws {@link AuthenticationError} on an invalid or expired token.
 * @throws {@link NetworkError} on connection failure.
 */
export async function* streamUser(
  transport: Transport,
  token: string,
  options: UserStreamOptions = {},
): AsyncGenerator<StreamMessage, void, undefined> {
  const sseOpts: SSEOptions = {
    token,
    ...(options.signal !== undefined ? { signal: options.signal } : {}),
  };

  for await (const data of transport.streamSSE(`${STREAM_BASE}/users`, sseOpts)) {
    yield parseStreamMessage(data);
  }
}
