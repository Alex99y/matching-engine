// Client-side input validation. These run before any network call and throw
// ValidationError on bad input, so obviously-wrong requests never hit the API.
// Pure functions — no I/O.

import { ValidationError } from "../errors/index.js";
import {
  OrderSide,
  OrderType,
  TimeInForce,
  type CreateOrderParams,
  type GetOrdersFilter,
  type LoginParams,
  type MarketStreamOptions,
  type RegisterParams,
} from "../types/index.js";

const MAX_BATCH_SIZE = 500;

const ORDER_SIDES = new Set<string>(Object.values(OrderSide));
const ORDER_TYPES = new Set<string>(Object.values(OrderType));
const TIME_IN_FORCES = new Set<string>(Object.values(TimeInForce));
const CLIENT_ORDER_ID_MIN = 32;
const CLIENT_ORDER_ID_MAX = 64;
const DATE_PATTERN = /^\d{4}-\d{2}-\d{2}$/;

function requireNonEmpty(value: string, field: string): void {
  if (typeof value !== "string" || value.length === 0) {
    throw new ValidationError(`${field} is required`);
  }
}

function requireNonNegative(value: bigint, field: string): void {
  if (value < 0n) {
    throw new ValidationError(`${field} must not be negative`);
  }
}

export function validateRegisterParams(params: RegisterParams): void {
  requireNonEmpty(params.username, "username");
  requireNonEmpty(params.email, "email");
  requireNonEmpty(params.password, "password");
}

export function validateLoginParams(params: LoginParams): void {
  requireNonEmpty(params.username, "username");
  requireNonEmpty(params.password, "password");
}

export function validateOrderId(orderId: string): void {
  requireNonEmpty(orderId, "orderId");
}

export function validateCreateOrderParams(params: CreateOrderParams): void {
  requireNonEmpty(params.market, "market");

  if (!ORDER_SIDES.has(params.side)) {
    throw new ValidationError(`invalid order side: ${String(params.side)}`);
  }
  if (!ORDER_TYPES.has(params.type)) {
    throw new ValidationError(`invalid order type: ${String(params.type)}`);
  }
  if (!TIME_IN_FORCES.has(params.timeInForce)) {
    throw new ValidationError(
      `invalid time in force: ${String(params.timeInForce)}`,
    );
  }

  if (params.clientOrderId !== undefined) {
    const len = params.clientOrderId.length;
    if (len < CLIENT_ORDER_ID_MIN || len > CLIENT_ORDER_ID_MAX) {
      throw new ValidationError(
        `clientOrderId must be ${CLIENT_ORDER_ID_MIN}-${CLIENT_ORDER_ID_MAX} characters`,
      );
    }
  }

  if (params.price !== undefined) {
    requireNonNegative(params.price, "price");
  }
  if (params.quantity !== undefined) {
    requireNonNegative(params.quantity, "quantity");
  }
  if (params.quoteQty !== undefined) {
    requireNonNegative(params.quoteQty, "quoteQty");
  }
}

export function validateBatchCreateOrderParams(params: CreateOrderParams[]): void {
  if (!Array.isArray(params) || params.length === 0) {
    throw new ValidationError("params must be a non-empty array");
  }
  if (params.length > MAX_BATCH_SIZE) {
    throw new ValidationError(`batch size exceeds maximum of ${MAX_BATCH_SIZE}`);
  }
  for (let i = 0; i < params.length; i++) {
    try {
      validateCreateOrderParams(params[i]!);
    } catch (e) {
      if (e instanceof ValidationError) {
        throw new ValidationError(`params[${i}]: ${e.message}`);
      }
      throw e;
    }
  }
}

export function validateBatchCancelOrderIds(orderIds: string[]): void {
  if (!Array.isArray(orderIds) || orderIds.length === 0) {
    throw new ValidationError("orderIds must be a non-empty array");
  }
  if (orderIds.length > MAX_BATCH_SIZE) {
    throw new ValidationError(`batch size exceeds maximum of ${MAX_BATCH_SIZE}`);
  }
  for (let i = 0; i < orderIds.length; i++) {
    if (typeof orderIds[i] !== "string" || orderIds[i]!.length === 0) {
      throw new ValidationError(`orderIds[${i}] must be a non-empty string`);
    }
  }
}

export function validateMarket(market: string): void {
  requireNonEmpty(market, "market");
}

export function validateMarketStreamOptions(options: MarketStreamOptions): void {
  if (options.group !== undefined && options.group <= 0n) {
    throw new ValidationError("group must be a positive integer");
  }
}

export function validateGetOrdersFilter(filter: GetOrdersFilter): void {
  if (filter.limit !== undefined) {
    if (!Number.isInteger(filter.limit) || filter.limit < 1 || filter.limit > 100) {
      throw new ValidationError("limit must be an integer between 1 and 100");
    }
  }
  if (filter.startDate !== undefined && !DATE_PATTERN.test(filter.startDate)) {
    throw new ValidationError("startDate must be in YYYY-MM-DD format");
  }
  if (filter.endDate !== undefined && !DATE_PATTERN.test(filter.endDate)) {
    throw new ValidationError("endDate must be in YYYY-MM-DD format");
  }
}
