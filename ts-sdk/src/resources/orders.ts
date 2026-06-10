// Orders resource: read and create orders. All routes require a bearer token.

import type { QueryParams, Transport } from "../http/transport.js";
import type {
  CreateOrderParams,
  CreateOrderResult,
  GetOrdersFilter,
  Order,
} from "../types/index.js";
import {
  parseCreateOrderResult,
  parseOrder,
  parseOrders,
} from "../utils/parse.js";
import {
  validateCreateOrderParams,
  validateGetOrdersFilter,
  validateOrderId,
} from "../utils/validation.js";

const ORDERS_BASE = "/api/v1/order";

export async function getOrder(
  transport: Transport,
  token: string,
  orderId: string,
): Promise<Order> {
  validateOrderId(orderId);
  const raw = await transport.request<unknown>(
    "GET",
    `${ORDERS_BASE}/${encodeURIComponent(orderId)}`,
    { token },
  );
  return parseOrder(raw);
}

export async function getOrders(
  transport: Transport,
  token: string,
  filter: GetOrdersFilter,
): Promise<Order[]> {
  validateGetOrdersFilter(filter);

  const query: QueryParams = {
    client_order_id: filter.clientOrderId,
    market: filter.market,
    start_date: filter.startDate,
    end_date: filter.endDate,
    limit: filter.limit,
  };
  // The API tests for the literal string "true", so only send when enabled.
  if (filter.showOpen) {
    query["show_open"] = "true";
  }
  if (filter.showCancelled) {
    query["show_cancelled"] = "true";
  }

  const raw = await transport.request<unknown>("GET", `${ORDERS_BASE}/`, {
    query,
    token,
  });
  return parseOrders(raw);
}

export async function cancelOrder(
  transport: Transport,
  token: string,
  orderId: string,
): Promise<void> {
  validateOrderId(orderId);
  await transport.request<void>(
    "DELETE",
    `${ORDERS_BASE}/${encodeURIComponent(orderId)}`,
    { token },
  );
}

export async function createOrder(
  transport: Transport,
  token: string,
  params: CreateOrderParams,
): Promise<CreateOrderResult> {
  validateCreateOrderParams(params);

  const body: Record<string, unknown> = {
    order_side: params.side,
    order_type: params.type,
    order_tif: params.timeInForce,
    market: params.market,
  };
  if (params.clientOrderId !== undefined) {
    body["client_order_id"] = params.clientOrderId;
  }
  if (params.price !== undefined) {
    body["price"] = params.price;
  }
  if (params.quantity !== undefined) {
    body["quantity"] = params.quantity;
  }
  if (params.quoteQty !== undefined) {
    body["quote_qty"] = params.quoteQty;
  }
  if (params.expiresAt !== undefined) {
    body["expires_at"] = params.expiresAt;
  }

  const raw = await transport.request<unknown>("POST", `${ORDERS_BASE}/`, {
    body,
    token,
  });
  return parseCreateOrderResult(raw);
}
