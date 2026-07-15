// Stream resource: SSE endpoints for live market data and private order updates.
//
// Routes (from api/internal/stream/router.go + server.go):
//   GET /api/v1/stream/:market  — public book + trade + heartbeat stream
//   GET /api/v1/stream/users    — private per-user order update stream (auth required)

import type { SSEOptions, Transport } from "../http/transport.js";
import type {
  CandleStreamMessage,
  CandleStreamOptions,
  MarketStreamOptions,
  StreamMessage,
  UserStreamOptions,
} from "../types/index.js";
import { parseCandleStreamMessage, parseStreamMessage } from "../utils/parse.js";
import {
  validateCandleStreamInterval,
  validateMarket,
  validateMarketStreamOptions,
} from "../utils/validation.js";

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
/**
 * Open a public SSE stream for candle updates on one market. The first frame
 * is always a `candle.snapshot` seeded from the current forming bucket.
 * Subsequent frames are `candle.trade` (one per match) and `candle.closed`
 * when a bucket boundary is crossed.
 *
 * The client is responsible for maintaining OHLCV state: `open` comes from
 * the snapshot and never changes within a bucket; `high`, `low`, `close`, and
 * `volume` are updated on each `candle.trade`.
 *
 * Break out of the loop or abort `options.signal` to close the connection.
 *
 * @param transport - SDK transport instance.
 * @param market - Market ref, e.g. `"ETH-USDT"`.
 * @param interval - Bucket size in seconds. Use {@link CandleInterval} constants.
 * @param options - Optional cancellation signal.
 * @throws {@link ValidationError} for an empty market or an invalid interval.
 * @throws {@link APIError} (404) for an unknown market.
 * @throws {@link NetworkError} on connection failure.
 * @example
 * for await (const msg of streamCandles(transport, "ETH-USDT", 60)) {
 *   if (msg.type === "candle.snapshot") console.log("open:", msg.open);
 *   if (msg.type === "candle.trade")    console.log("trade price:", msg.price);
 *   if (msg.type === "candle.closed")   console.log("bucket closed:", msg.bucketStart);
 * }
 */
export async function* streamCandles(
  transport: Transport,
  market: string,
  interval: number,
  options: CandleStreamOptions = {},
): AsyncGenerator<CandleStreamMessage, void, undefined> {
  validateMarket(market);
  validateCandleStreamInterval(interval);

  const sseOpts: SSEOptions = {
    query: { interval },
    ...(options.signal !== undefined ? { signal: options.signal } : {}),
  };

  for await (const data of transport.streamSSE(`${STREAM_BASE}/markets/${market}/candles`, sseOpts)) {
    yield parseCandleStreamMessage(data);
  }
}

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
