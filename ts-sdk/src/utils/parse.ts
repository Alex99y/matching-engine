// Pure parsers that validate untrusted API responses and map the snake_case
// wire shape onto the SDK's public camelCase types. Anything that does not
// match the expected shape becomes a ParseError — the server response type is
// a promise, not a guarantee. No I/O here.

import { ParseError } from "../errors/index.js";
import type {
  CancelledOrder,
  CreateOrderResult,
  Instrument,
  Market,
  OpenOrder,
  Order,
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
  const expiresAt = optNumber(o, "expires_at");
  const openOrder = o["open_order"] != null ? parseOpenOrder(o["open_order"]) : undefined;
  const cancelledOrder =
    o["cancelled_order"] != null ? parseCancelledOrder(o["cancelled_order"]) : undefined;

  return {
    ...order,
    ...(clientOrderId !== undefined ? { clientOrderId } : {}),
    ...(expiresAt !== undefined ? { expiresAt } : {}),
    ...(openOrder !== undefined ? { openOrder } : {}),
    ...(cancelledOrder !== undefined ? { cancelledOrder } : {}),
  };
}

export function parseOrders(raw: unknown): Order[] {
  return asArray(raw, "orders").map(parseOrder);
}

export function parseCreateOrderResult(raw: unknown): CreateOrderResult {
  const o = asRecord(raw, "create order result");
  return { orderId: reqString(o, "order_id") };
}

export function parseLoginToken(raw: unknown): string {
  const o = asRecord(raw, "login response");
  return reqString(o, "token");
}
