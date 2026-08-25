// Pure parsers that validate untrusted API responses and map the snake_case
// wire shape onto the SDK's public camelCase types. Anything that does not
// match the expected shape becomes a ParseError — the server response type is
// a promise, not a guarantee. No I/O here.

import { ParseError } from "../errors/index.js";
import type {
  Balance,
  BatchCancelOrderResponse,
  BatchCancelOrderResult,
  BatchCreateOrderResponse,
  BatchCreateOrderResult,
  BookLevel,
  BookMessage,
  Candle,
  CancelledOrder,
  CandleClosedMessage,
  CandleSnapshotMessage,
  CandleStreamMessage,
  CandleTradeMessage,
  CreateTokenResult,
  FaucetResult,
  GetCandlesResponse,
  HeartbeatMessage,
  Instrument,
  Market,
  MarketDepth,
  MarketPrice,
  Match,
  OpenOrder,
  Operation,
  OperationType,
  Order,
  OrderMatch,
  OrderMessage,
  RefreshSessionResult,
  Session,
  SnapshotMessage,
  StreamMessage,
  TradeMessage,
} from "../types/index.js";

function asRecord(value: unknown, what: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new ParseError(`expected ${what} to be an object`);
  }
  return value as Record<string, unknown>;
}

function asArray(value: unknown, what: string): unknown[] {
  if (!Array.isArray(value)) {
    throw new ParseError(`expected ${what} to be an array`);
  }
  return value;
}

function reqString(obj: Record<string, unknown>, key: string): string {
  const v = obj[key];
  if (typeof v !== "string") {
    throw new ParseError(`expected field "${key}" to be a string`);
  }
  return v;
}

function optString(obj: Record<string, unknown>, key: string): string | undefined {
  const v = obj[key];
  if (v === undefined || v === null) {
    return undefined;
  }
  if (typeof v !== "string") {
    throw new ParseError(`expected field "${key}" to be a string`);
  }
  return v;
}

function reqNumber(obj: Record<string, unknown>, key: string): number {
  const v = obj[key];
  if (typeof v !== "number" || !Number.isFinite(v)) {
    throw new ParseError(`expected field "${key}" to be a number`);
  }
  return v;
}

function optNumber(obj: Record<string, unknown>, key: string): number | undefined {
  const v = obj[key];
  if (v === undefined || v === null) {
    return undefined;
  }
  if (typeof v !== "number" || !Number.isFinite(v)) {
    throw new ParseError(`expected field "${key}" to be a number`);
  }
  return v;
}

function reqBool(obj: Record<string, unknown>, key: string): boolean {
  const v = obj[key];
  if (typeof v !== "boolean") {
    throw new ParseError(`expected field "${key}" to be a boolean`);
  }
  return v;
}

function reqBigInt(obj: Record<string, unknown>, key: string): bigint {
  const v = obj[key];
  // parseWithBigInts already decodes these as bigint; accept number as a
  // defensive fallback for runtimes without source-text reviver support.
  if (typeof v === "bigint") {
    return v;
  }
  if (typeof v === "number" && Number.isInteger(v)) {
    return BigInt(v);
  }
  throw new ParseError(`expected field "${key}" to be an integer`);
}

export function parseInstrument(raw: unknown): Instrument {
  const o = asRecord(raw, "instrument");
  return {
    name: reqString(o, "name"),
    symbol: reqString(o, "symbol"),
    decimals: reqNumber(o, "decimals"),
    createdAt: reqString(o, "created_at"),
  };
}

export function parseInstruments(raw: unknown): Instrument[] {
  return asArray(raw, "instruments").map(parseInstrument);
}

export function parseMarket(raw: unknown): Market {
  const o = asRecord(raw, "market");
  return {
    baseSymbol: reqString(o, "base_symbol"),
    quoteSymbol: reqString(o, "quote_symbol"),
    priceQuantum: reqBigInt(o, "price_quantum"),
    amountQuantum: reqBigInt(o, "amount_quantum"),
    minOrderSize: reqBigInt(o, "min_order_size"),
    maxOrderSize: reqBigInt(o, "max_order_size"),
  };
}

export function parseMarkets(raw: unknown): Market[] {
  return asArray(raw, "markets").map(parseMarket);
}

function parseDepthLevel(raw: unknown): BookLevel {
  const o = asRecord(raw, "depth level");
  return { price: reqBigInt(o, "price"), quantity: reqBigInt(o, "quantity") };
}

export function parseMarketDepth(raw: unknown): MarketDepth {
  const o = asRecord(raw, "market depth");
  return {
    market: reqString(o, "market"),
    bids: asArray(o["bids"], "bids").map(parseDepthLevel),
    asks: asArray(o["asks"], "asks").map(parseDepthLevel),
  };
}

// GetPrices amounts arrive as nullable decimal strings (FormatUint64Ptr in
// Go) — same wire convention as candles, but optional since a market can have
// no matches yet (ever, or just within the 24h window). "price" is also a
// BIGINT_WIRE_FIELDS key (reused, as a raw number, by orders/matches/depth),
// so transport.request's reviver already turns it into a bigint before this
// runs; its siblings (min/max/volume) aren't flagged and arrive as plain
// strings. Accept both shapes rather than picking one.
function optBigIntStr(obj: Record<string, unknown>, key: string): bigint | undefined {
  const v = obj[key];
  if (v === undefined || v === null) {
    return undefined;
  }
  if (typeof v === "bigint") {
    return v;
  }
  if (typeof v !== "string" || v === "") {
    throw new ParseError(`expected field "${key}" to be a decimal string`);
  }
  try {
    return BigInt(v);
  } catch {
    throw new ParseError(`expected field "${key}" to be a valid integer string`);
  }
}

function parseMarketPrice(raw: unknown): MarketPrice {
  const o = asRecord(raw, "market price");
  const price = optBigIntStr(o, "price");
  const minPrice24h = optBigIntStr(o, "min_price_24h");
  const maxPrice24h = optBigIntStr(o, "max_price_24h");
  const volume24h = optBigIntStr(o, "volume_24h");
  const changePercent24h = optString(o, "change_percent_24h");
  return {
    market: reqString(o, "market"),
    ...(price !== undefined ? { price } : {}),
    ...(minPrice24h !== undefined ? { minPrice24h } : {}),
    ...(maxPrice24h !== undefined ? { maxPrice24h } : {}),
    ...(volume24h !== undefined ? { volume24h } : {}),
    ...(changePercent24h !== undefined ? { changePercent24h } : {}),
  };
}

export function parseMarketPrices(raw: unknown): MarketPrice[] {
  return asArray(raw, "market prices").map(parseMarketPrice);
}

function parseMatch(raw: unknown): Match {
  const o = asRecord(raw, "match");
  return {
    id: reqString(o, "id"),
    price: reqBigInt(o, "price"),
    quantity: reqBigInt(o, "quantity"),
    takerSide: reqSide(o, "taker_side"),
    matchTime: reqNumber(o, "match_time"),
  };
}

export function parseMatches(raw: unknown): Match[] {
  return asArray(raw, "matches").map(parseMatch);
}

function parseOpenOrder(raw: unknown): OpenOrder {
  const o = asRecord(raw, "open_order");
  return {
    price: reqBigInt(o, "price"),
    side: reqString(o, "side"),
    remainingHave: reqBigInt(o, "remaining_have"),
    remainingWant: reqBigInt(o, "remaining_want"),
  };
}

function parseCancelledOrder(raw: unknown): CancelledOrder {
  const o = asRecord(raw, "cancelled_order");
  return {
    cancelledAt: reqNumber(o, "cancelled_at"),
    remainingHave: reqBigInt(o, "remaining_have"),
    remainingWant: reqBigInt(o, "remaining_want"),
  };
}

function parseOrderMatch(raw: unknown): OrderMatch {
  const o = asRecord(raw, "order match");
  return {
    id: reqString(o, "id"),
    price: reqBigInt(o, "price"),
    baseAmount: reqBigInt(o, "base_amount"),
    quoteAmount: reqBigInt(o, "quote_amount"),
    fee: reqBigInt(o, "fee"),
    isTaker: reqBool(o, "is_taker"),
    matchTime: reqNumber(o, "match_time"),
  };
}

export function parseOrder(raw: unknown): Order {
  const o = asRecord(raw, "order");
  const order: Order = {
    id: reqString(o, "id"),
    type: reqString(o, "type"),
    timeInForce: reqString(o, "time_in_force"),
    haveQuantity: reqBigInt(o, "have_quantity"),
    wantQuantity: reqBigInt(o, "want_quantity"),
    createdAt: reqNumber(o, "created_at"),
  };

  const clientOrderId = optString(o, "client_order_id");
  const side = optString(o, "side");
  const expiresAt = optNumber(o, "expires_at");
  const openOrder = o["open_order"] != null ? parseOpenOrder(o["open_order"]) : undefined;
  const cancelledOrder =
    o["cancelled_order"] != null ? parseCancelledOrder(o["cancelled_order"]) : undefined;
  const matches = o["matches"] != null ? asArray(o["matches"], "matches").map(parseOrderMatch) : undefined;

  return {
    ...order,
    ...(clientOrderId !== undefined ? { clientOrderId } : {}),
    ...(side !== undefined ? { side } : {}),
    ...(expiresAt !== undefined ? { expiresAt } : {}),
    ...(openOrder !== undefined ? { openOrder } : {}),
    ...(cancelledOrder !== undefined ? { cancelledOrder } : {}),
    ...(matches !== undefined ? { matches } : {}),
  };
}

export function parseOrders(raw: unknown): Order[] {
  return asArray(raw, "orders").map(parseOrder);
}

export function parseBatchCreateOrderResult(raw: unknown): BatchCreateOrderResult {
  const o = asRecord(raw, "batch create order result");
  const index = reqNumber(o, "index");
  const orderId = optString(o, "order_id");
  const error = optString(o, "error");
  return {
    index,
    ...(orderId !== undefined ? { orderId } : {}),
    ...(error !== undefined ? { error } : {}),
  };
}

export function parseBatchCreateOrderResponse(raw: unknown): BatchCreateOrderResponse {
  const o = asRecord(raw, "batch create order response");
  return {
    results: asArray(o["results"], "results").map(parseBatchCreateOrderResult),
  };
}

export function parseBatchCancelOrderResult(raw: unknown): BatchCancelOrderResult {
  const o = asRecord(raw, "batch cancel order result");
  const base = { orderId: reqString(o, "order_id") };
  const error = optString(o, "error");
  return error !== undefined ? { ...base, error } : base;
}

export function parseBatchCancelOrderResponse(raw: unknown): BatchCancelOrderResponse {
  const o = asRecord(raw, "batch cancel order response");
  return {
    results: asArray(o["results"], "results").map(parseBatchCancelOrderResult),
  };
}

// ---- Stream message parsers ----
//
// SSE amounts (price, quantity, filled, remaining) arrive as decimal strings —
// the Go edge serialises uint64 with strconv.FormatUint. Parse with BigInt()
// directly; the BIGINT_WIRE_FIELDS reviver is for REST JSON numbers only.

const SIDES = new Set(["buy", "sell"]);

function reqBigIntStr(obj: Record<string, unknown>, key: string): bigint {
  const v = obj[key];
  if (typeof v !== "string" || v === "") {
    throw new ParseError(`expected field "${key}" to be a decimal string`);
  }
  try {
    return BigInt(v);
  } catch {
    throw new ParseError(`expected field "${key}" to be a valid integer string`);
  }
}

function reqSide(obj: Record<string, unknown>, key: string): "buy" | "sell" {
  const v = reqString(obj, key);
  if (!SIDES.has(v)) {
    throw new ParseError(`expected field "${key}" to be "buy" or "sell", got "${v}"`);
  }
  return v as "buy" | "sell";
}

function parseBookLevel(raw: unknown): BookLevel {
  const o = asRecord(raw, "book level");
  return { price: reqBigIntStr(o, "price"), quantity: reqBigIntStr(o, "quantity") };
}

/**
 * Parse one SSE `data:` payload string into a typed {@link StreamMessage}.
 * @throws {@link ParseError} on malformed JSON or an unexpected shape.
 */
export function parseStreamMessage(data: string): StreamMessage {
  let raw: unknown;
  try {
    raw = JSON.parse(data);
  } catch (err) {
    throw new ParseError("invalid SSE message JSON", err);
  }

  const o = asRecord(raw, "stream message");
  const type = reqString(o, "type");

  switch (type) {
    case "snapshot": {
      const result: SnapshotMessage = {
        type: "snapshot",
        market: reqString(o, "market"),
        bids: asArray(o["bids"], "bids").map(parseBookLevel),
        asks: asArray(o["asks"], "asks").map(parseBookLevel),
      };
      return result;
    }
    case "book": {
      const result: BookMessage = {
        type: "book",
        side: reqSide(o, "side"),
        price: reqBigIntStr(o, "price"),
        quantity: reqBigIntStr(o, "quantity"),
      };
      return result;
    }
    case "trade": {
      const result: TradeMessage = {
        type: "trade",
        price: reqBigIntStr(o, "price"),
        quantity: reqBigIntStr(o, "quantity"),
        takerSide: reqSide(o, "taker_side"),
      };
      return result;
    }
    case "heartbeat": {
      const result: HeartbeatMessage = { type: "heartbeat" };
      return result;
    }
    case "order": {
      const result: OrderMessage = {
        type: "order",
        orderId: reqString(o, "order_id"),
        status: reqString(o, "status"),
        filled: reqBigIntStr(o, "filled"),
        remaining: reqBigIntStr(o, "remaining"),
      };
      return result;
    }
    default:
      throw new ParseError(`unknown stream message type: "${type}"`);
  }
}

export function parseLoginToken(raw: unknown): string {
  const o = asRecord(raw, "login response");
  return reqString(o, "token");
}

export function parseSession(raw: unknown): Session {
  const o = asRecord(raw, "session");
  const session: Session = {
    sessionId: reqString(o, "session_id"),
    createdAt: reqNumber(o, "created_at"),
    expiresAt: reqNumber(o, "expires_at"),
    origin: reqString(o, "origin") as Session["origin"],
    scope: reqString(o, "scope") as Session["scope"],
  };
  const userAgent = optString(o, "user_agent");
  const ipAddress = optString(o, "ip_address");
  return {
    ...session,
    ...(userAgent !== undefined ? { userAgent } : {}),
    ...(ipAddress !== undefined ? { ipAddress } : {}),
  };
}

export function parseActiveSessions(raw: unknown): Session[] {
  return asArray(raw, "sessions").map(parseSession);
}

export function parseCreateTokenResult(raw: unknown): CreateTokenResult {
  const o = asRecord(raw, "create token response");
  return {
    token: reqString(o, "token"),
    scope: reqString(o, "scope") as CreateTokenResult["scope"],
    expiresAt: reqNumber(o, "expires_at"),
  };
}

export function parseRefreshSessionResult(raw: unknown): RefreshSessionResult {
  const o = asRecord(raw, "refresh session response");
  return {
    expiresAt: reqNumber(o, "expires_at"),
  };
}

function parseBalance(raw: unknown): Balance {
  const o = asRecord(raw, "balance");
  return {
    name: reqString(o, "name"),
    symbol: reqString(o, "symbol"),
    decimals: reqNumber(o, "decimals"),
    balance: reqBigInt(o, "balance"),
    blocked: reqBigInt(o, "blocked"),
    frozen: reqBigInt(o, "frozen"),
  };
}

export function parseBalances(raw: unknown): Balance[] {
  return asArray(raw, "balances").map(parseBalance);
}

function parseOperation(raw: unknown): Operation {
  const o = asRecord(raw, "operation");
  const operation: {
    id: string;
    symbol: string;
    amount: bigint;
    type: OperationType;
    reason?: string;
    createdAt: number;
  } = {
    id: reqString(o, "id"),
    symbol: reqString(o, "symbol"),
    amount: reqBigInt(o, "amount"),
    type: reqString(o, "type") as OperationType,
    createdAt: reqNumber(o, "created_at"),
  };
  const reason = optString(o, "reason");
  if (reason !== undefined) operation.reason = reason;
  return operation;
}

export function parseOperations(raw: unknown): Operation[] {
  return asArray(raw, "operations").map(parseOperation);
}

export function parseFaucetResult(raw: unknown): FaucetResult {
  const o = asRecord(raw, "faucet response");
  return {
    symbol: reqString(o, "symbol"),
    amount: reqBigInt(o, "amount"),
  };
}

// ---- Candle parsers ----
//
// OHLCV amounts arrive as decimal strings in both the REST and SSE candle
// responses (serialized via FormatUint64 in Go). Parse with reqBigIntStr —
// no BIGINT_WIRE_FIELDS entries needed.

export function parseCandle(raw: unknown): Candle {
  const o = asRecord(raw, "candle");
  return {
    bucketStart: reqNumber(o, "bucket_start"),
    open: reqBigIntStr(o, "open"),
    high: reqBigIntStr(o, "high"),
    low: reqBigIntStr(o, "low"),
    close: reqBigIntStr(o, "close"),
    volume: reqBigIntStr(o, "volume"),
  };
}

export function parseGetCandlesResponse(raw: unknown): GetCandlesResponse {
  const o = asRecord(raw, "candles response");
  return {
    interval: reqNumber(o, "interval"),
    candles: asArray(o["candles"], "candles").map(parseCandle),
  };
}

/**
 * Parse one SSE `data:` payload string into a typed {@link CandleStreamMessage}.
 * @throws {@link ParseError} on malformed JSON or an unexpected shape.
 */
export function parseCandleStreamMessage(data: string): CandleStreamMessage {
  let raw: unknown;
  try {
    raw = JSON.parse(data);
  } catch (err) {
    throw new ParseError("invalid candle SSE message JSON", err);
  }

  const o = asRecord(raw, "candle stream message");
  const type = reqString(o, "type");

  switch (type) {
    case "candle.snapshot": {
      const result: CandleSnapshotMessage = {
        type: "candle.snapshot",
        interval: reqNumber(o, "interval"),
        bucketStart: reqNumber(o, "bucket_start"),
        open: reqBigIntStr(o, "open"),
        high: reqBigIntStr(o, "high"),
        low: reqBigIntStr(o, "low"),
        close: reqBigIntStr(o, "close"),
        volume: reqBigIntStr(o, "volume"),
      };
      return result;
    }
    case "candle.trade": {
      const result: CandleTradeMessage = {
        type: "candle.trade",
        time: reqNumber(o, "time"),
        price: reqBigIntStr(o, "price"),
        quantity: reqBigIntStr(o, "quantity"),
        takerSide: reqSide(o, "taker_side"),
      };
      return result;
    }
    case "candle.closed": {
      const result: CandleClosedMessage = {
        type: "candle.closed",
        interval: reqNumber(o, "interval"),
        bucketStart: reqNumber(o, "bucket_start"),
      };
      return result;
    }
    default:
      throw new ParseError(`unknown candle stream message type: "${type}"`);
  }
}
