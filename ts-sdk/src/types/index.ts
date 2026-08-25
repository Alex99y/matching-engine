// Public types and enums for the matching-engine SDK. No logic lives here.
//
// uint64 fields from the API are exposed as `bigint` to avoid the precision
// loss `number` suffers above 2^53. Timestamps (unix seconds) stay `number`
// since they comfortably fit and bots reason about them as plain integers.

// ---- Wire enums (const objects, not TS enums; see typescript-strict rule) ----

export const OrderSide = {
  Buy: "buy",
  Sell: "sell",
} as const;
export type OrderSide = (typeof OrderSide)[keyof typeof OrderSide];

export const OrderType = {
  Limit: "limit",
  Market: "market",
} as const;
export type OrderType = (typeof OrderType)[keyof typeof OrderType];

export const TimeInForce = {
  GoodTillCancel: "gtc",
  ImmediateOrCancel: "ioc",
  FillOrKill: "fok",
} as const;
export type TimeInForce = (typeof TimeInForce)[keyof typeof TimeInForce];

// ---- Public (unauthenticated) request params ----

export interface RegisterParams {
  readonly username: string;
  readonly email: string;
  readonly password: string;
}

export interface LoginParams {
  readonly username: string;
  readonly password: string;
}

// ---- Public responses ----

export interface Instrument {
  readonly name: string;
  readonly symbol: string;
  readonly decimals: number;
  /** RFC3339 timestamp, as serialized by Go's time.Time. */
  readonly createdAt: string;
}

export interface Market {
  readonly baseSymbol: string;
  readonly quoteSymbol: string;
  readonly priceQuantum: bigint;
  readonly amountQuantum: bigint;
  readonly minOrderSize: bigint;
  readonly maxOrderSize: bigint;
}

export interface GetDepthOptions {
  /**
   * Price-bucket grouping size in price units. Must be a positive multiple of
   * the market's `priceQuantum`. Defaults to native resolution when omitted.
   */
  readonly group?: bigint;
}

/**
 * One-shot order-book snapshot — the REST counterpart of {@link streamMarket}'s
 * first frame. bids are sorted high-to-low, asks low-to-high; price/quantity
 * are in the market's quantum units, same as {@link Order} amounts.
 */
export interface MarketDepth {
  readonly market: string;
  readonly bids: readonly BookLevel[];
  readonly asks: readonly BookLevel[];
}

/**
 * Latest price and 24h stats for one market. `price` is undefined when the
 * market has never matched a trade; `minPrice24h`/`maxPrice24h`/`volume24h`/
 * `changePercent24h` are undefined when it has no matches within the last 24h
 * even if `price` is set (it can reflect an older trade).
 */
export interface MarketPrice {
  readonly market: string;
  readonly price?: bigint;
  readonly minPrice24h?: bigint;
  readonly maxPrice24h?: bigint;
  readonly volume24h?: bigint;
  /** Formatted to 2 decimals server-side, e.g. `"-3.14"`. */
  readonly changePercent24h?: string;
}

/**
 * One historical trade — the REST counterpart of the `"trade"` message on
 * {@link streamMarket}'s public feed. Deliberately has no order id fields:
 * this is public, unauthenticated market data, and a match's buy/sell orders
 * belong to two different users.
 */
export interface Match {
  readonly id: string;
  readonly price: bigint;
  readonly quantity: bigint;
  readonly takerSide: "buy" | "sell";
  /** Unix seconds. */
  readonly matchTime: number;
}

export interface GetMatchesFilter {
  /** YYYY-MM-DD (inclusive lower bound). */
  readonly startDate?: string;
  /** YYYY-MM-DD (exclusive upper bound: returns matches where matchTime < endDate). */
  readonly endDate?: string;
  /** 1-100. The API defaults to 100 when omitted. */
  readonly limit?: number;
}

// ---- Private (authenticated) order params ----

export interface CreateOrderParams {
  readonly market: string;
  readonly side: OrderSide;
  readonly type: OrderType;
  readonly timeInForce: TimeInForce;
  /** Optional idempotency key. The API requires 32-64 chars when present. */
  readonly clientOrderId?: string;
  /** Required for limit orders; ignored by market orders. uint64. */
  readonly price?: bigint;
  /** Base-asset quantity. uint64. */
  readonly quantity?: bigint;
  /** Quote-asset budget for market buys. uint64. */
  readonly quoteQty?: bigint;
  /** Unix seconds; only valid together with a non-GTC time-in-force. */
  readonly expiresAt?: number;
}

// ---- Batch order results ----

export interface BatchCreateOrderResult {
  readonly index: number;
  readonly orderId?: string;
  readonly error?: string;
}

export interface BatchCreateOrderResponse {
  readonly results: readonly BatchCreateOrderResult[];
}

export interface BatchCancelOrderResult {
  readonly orderId: string;
  readonly error?: string;
}

export interface BatchCancelOrderResponse {
  readonly results: readonly BatchCancelOrderResult[];
}

export interface GetOrdersFilter {
  /** Exact match. When set, the API returns at most one order. */
  readonly clientOrderId?: string;
  readonly market?: string;
  /** YYYY-MM-DD (inclusive lower bound). */
  readonly startDate?: string;
  /** YYYY-MM-DD (exclusive upper bound: returns orders where created_at < endDate). */
  readonly endDate?: string;
  /** 1-100. The API defaults to 10 when omitted. */
  readonly limit?: number;
  readonly showOpen?: boolean;
  readonly showCancelled?: boolean;
}

// ---- Authenticated session responses ----

export const SessionOrigin = {
  /** Password-authenticated. Can mint tokens and revoke the user's other sessions. */
  Login: "login",
  /** Created via {@link CreateTokenParams}. A dead end: acts per its scope, can never
   *  mint another token or touch a session other than itself. */
  Minted: "minted",
} as const;
export type SessionOrigin = (typeof SessionOrigin)[keyof typeof SessionOrigin];

export const SessionScope = {
  /** Cannot place or cancel orders. */
  Read: "read",
  /** Can place and cancel orders. A login session is always "write". */
  Write: "write",
} as const;
export type SessionScope = (typeof SessionScope)[keyof typeof SessionScope];

export interface Session {
  /** One-way hash of the bearer token, reused as the external session id — never accept this for authentication. */
  readonly sessionId: string;
  /** Unix seconds. */
  readonly createdAt: number;
  /** Unix seconds. */
  readonly expiresAt: number;
  readonly origin: SessionOrigin;
  readonly scope: SessionScope;
  /** Best-effort request metadata captured when the session was created; absent for
   *  sessions created before this field existed. */
  readonly userAgent?: string;
  /** Best-effort request metadata captured when the session was created; absent for
   *  sessions created before this field existed. */
  readonly ipAddress?: string;
}

export interface CreateTokenParams {
  readonly scope: SessionScope;
}

export interface CreateTokenResult {
  readonly token: string;
  readonly scope: SessionScope;
  /** Unix seconds. */
  readonly expiresAt: number;
}

export interface RefreshSessionResult {
  /** Unix seconds — the session's new expiry. */
  readonly expiresAt: number;
}

// ---- Authenticated user responses ----

export interface Balance {
  readonly name: string;
  readonly symbol: string;
  readonly decimals: number;
  /** Tradeable amount (excludes blocked and frozen). uint64. */
  readonly balance: bigint;
  /** Amount currently locked in the user's own open orders. uint64. */
  readonly blocked: bigint;
  /** Amount frozen by an admin; unavailable for trading or withdrawal. uint64. */
  readonly frozen: bigint;
}

export const OperationType = {
  Deposit: "deposit",
  Withdraw: "withdraw",
  Freeze: "freeze",
  Unfreeze: "unfreeze",
} as const;
export type OperationType = (typeof OperationType)[keyof typeof OperationType];

/**
 * A single deposit/withdraw/freeze/unfreeze applied to the user's balance.
 * These are only ever written by an admin via the CLI — there is no
 * user-facing way to create one.
 */
export interface Operation {
  readonly id: string;
  readonly symbol: string;
  /** Always positive; {@link type} conveys direction. uint64. */
  readonly amount: bigint;
  readonly type: OperationType;
  /** Optional operator note. Absent when none was given. */
  readonly reason?: string;
  /** Unix seconds. */
  readonly createdAt: number;
}

export interface GetUserOperationsFilter {
  /** YYYY-MM-DD (inclusive lower bound). */
  readonly startDate?: string;
  /** YYYY-MM-DD (exclusive upper bound: returns operations where createdAt < endDate). */
  readonly endDate?: string;
  /** 1-100. The API defaults to 100 when omitted. */
  readonly limit?: number;
}

/**
 * Result of a sandbox/POC faucet call. The endpoint has no rate limit or
 * lifetime cap — call it repeatedly to accumulate balance.
 */
export interface FaucetResult {
  readonly symbol: string;
  /** Amount credited, in the instrument's smallest unit. uint64. */
  readonly amount: bigint;
}

// ---- Stream event types ----

export const OrderStatus = {
  Open: "open",
  Filled: "filled",
  PartiallyFilled: "partially_filled",
  Cancelled: "cancelled",
  Rejected: "rejected",
} as const;
export type OrderStatus = (typeof OrderStatus)[keyof typeof OrderStatus];

export interface BookLevel {
  readonly price: bigint;
  readonly quantity: bigint;
}

export interface SnapshotMessage {
  readonly type: "snapshot";
  readonly market: string;
  readonly bids: readonly BookLevel[];
  readonly asks: readonly BookLevel[];
}

export interface BookMessage {
  readonly type: "book";
  readonly side: "buy" | "sell";
  /** Price bucket. `quantity === 0n` means the level was removed. */
  readonly price: bigint;
  readonly quantity: bigint;
}

export interface TradeMessage {
  readonly type: "trade";
  readonly price: bigint;
  readonly quantity: bigint;
  readonly takerSide: "buy" | "sell";
}

export interface HeartbeatMessage {
  readonly type: "heartbeat";
}

export interface OrderMessage {
  readonly type: "order";
  readonly orderId: string;
  readonly status: string;
  readonly filled: bigint;
  readonly remaining: bigint;
}

/** Discriminated union of every message the SSE stream can emit. */
export type StreamMessage =
  | SnapshotMessage
  | BookMessage
  | TradeMessage
  | HeartbeatMessage
  | OrderMessage;

export interface MarketStreamOptions {
  /**
   * Price-bucket grouping size in price units. Must be a positive multiple of
   * the market's `priceQuantum`. Defaults to native resolution when omitted.
   */
  readonly group?: bigint;
  /** Cancellation signal. Abort it to close the stream. */
  readonly signal?: AbortSignal;
}

export interface UserStreamOptions {
  /** Cancellation signal. Abort it to close the stream. */
  readonly signal?: AbortSignal;
}

// ---- Candle intervals ----

export const CandleInterval = {
  OneMinute:      60,
  FiveMinutes:   300,
  FifteenMinutes: 900,
  OneHour:      3600,
  FourHours:   14400,
  OneDay:      86400,
} as const;
export type CandleInterval = (typeof CandleInterval)[keyof typeof CandleInterval];

// ---- Candle REST types ----

export interface Candle {
  readonly bucketStart: number;
  readonly open: bigint;
  readonly high: bigint;
  readonly low: bigint;
  readonly close: bigint;
  readonly volume: bigint;
}

export interface GetCandlesParams {
  readonly interval: CandleInterval;
  /** Unix seconds (inclusive lower bound). */
  readonly from: number;
  /** Unix seconds (exclusive upper bound). */
  readonly to: number;
}

export interface GetCandlesResponse {
  readonly interval: number;
  readonly candles: readonly Candle[];
}

// ---- Candle SSE event types ----

export interface CandleSnapshotMessage {
  readonly type: "candle.snapshot";
  readonly interval: number;
  readonly bucketStart: number;
  readonly open: bigint;
  readonly high: bigint;
  readonly low: bigint;
  readonly close: bigint;
  readonly volume: bigint;
}

export interface CandleTradeMessage {
  readonly type: "candle.trade";
  /** Unix seconds. */
  readonly time: number;
  readonly price: bigint;
  readonly quantity: bigint;
  readonly takerSide: "buy" | "sell";
}

export interface CandleClosedMessage {
  readonly type: "candle.closed";
  readonly interval: number;
  readonly bucketStart: number;
}

/** Discriminated union of every message the candle SSE stream can emit. */
export type CandleStreamMessage =
  | CandleSnapshotMessage
  | CandleTradeMessage
  | CandleClosedMessage;

export interface CandleStreamOptions {
  /** Cancellation signal. Abort it to close the stream. */
  readonly signal?: AbortSignal;
}

// ---- Order responses ----

export interface OpenOrder {
  readonly price: bigint;
  readonly side: string;
  readonly remainingHave: bigint;
  readonly remainingWant: bigint;
}

export interface CancelledOrder {
  readonly cancelledAt: number;
  readonly remainingHave: bigint;
  readonly remainingWant: bigint;
}

/**
 * One fill against an order. Amounts are symmetric (base/quote traded in that
 * match, not oriented by the order's own side); `fee` is denominated in
 * whatever the order received — base if it bought, quote if it sold.
 */
export interface OrderMatch {
  readonly id: string;
  readonly price: bigint;
  readonly baseAmount: bigint;
  readonly quoteAmount: bigint;
  readonly fee: bigint;
  readonly isTaker: boolean;
  /** Unix seconds. */
  readonly matchTime: number;
}

export interface Order {
  readonly id: string;
  readonly clientOrderId?: string;
  readonly type: string;
  readonly timeInForce: string;
  /** "buy" or "sell". Absent only if the order's market has since been deleted. */
  readonly side?: string;
  readonly haveQuantity: bigint;
  readonly wantQuantity: bigint;
  readonly createdAt: number;
  readonly expiresAt?: number;
  readonly openOrder?: OpenOrder;
  readonly cancelledOrder?: CancelledOrder;
  /** Only populated by {@link getOrder} (single-order fetch), never by {@link getOrders}. */
  readonly matches?: readonly OrderMatch[];
}
