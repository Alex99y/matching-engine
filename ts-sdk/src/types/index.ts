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

export interface CreateOrderResult {
  readonly orderId: string;
}

export interface GetOrdersFilter {
  /** Exact match. When set, the API returns at most one order. */
  readonly clientOrderId?: string;
  readonly market?: string;
  /** YYYY-MM-DD (inclusive lower bound). */
  readonly startDate?: string;
  /** YYYY-MM-DD (inclusive upper bound). */
  readonly endDate?: string;
  /** 1-100. The API defaults to 10 when omitted. */
  readonly limit?: number;
  readonly showOpen?: boolean;
  readonly showCancelled?: boolean;
}

// ---- Authenticated user responses ----

export interface Balance {
  readonly name: string;
  readonly symbol: string;
  readonly decimals: number;
  /** Available (unlocked) amount. uint64. */
  readonly balance: bigint;
  /** Amount currently locked in open orders. uint64. */
  readonly blocked: bigint;
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

export interface Order {
  readonly id: string;
  readonly clientOrderId?: string;
  readonly type: string;
  readonly timeInForce: string;
  readonly haveQuantity: bigint;
  readonly wantQuantity: bigint;
  readonly createdAt: number;
  readonly expiresAt?: number;
  readonly openOrder?: OpenOrder;
  readonly cancelledOrder?: CancelledOrder;
}
