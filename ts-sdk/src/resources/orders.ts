// Orders resource: read and create orders. All routes require a bearer token.

import type { Transport } from "../http/transport.js";
import type {
  BatchCancelOrderResponse,
  BatchCreateOrderResponse,
  CreateOrderParams,
  GetOrdersFilter,
  Order,
} from "../types/index.js";
import {
  parseBatchCancelOrderResponse,
  parseBatchCreateOrderResponse,
  parseOrder,
  parseOrders,
} from "../utils/parse.js";
import {
  validateBatchCancelOrderIds,
  validateBatchCreateOrderParams,
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

  const query: Record<string, string | number | boolean | undefined> = {
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

export async function createOrders(
  transport: Transport,
  token: string,
  params: CreateOrderParams[],
): Promise<BatchCreateOrderResponse> {
  validateBatchCreateOrderParams(params);

  const body = params.map((p) => {
    const item: Record<string, unknown> = {
      order_side: p.side,
      order_type: p.type,
      order_tif: p.timeInForce,
      market: p.market,
    };
    if (p.clientOrderId !== undefined) item["client_order_id"] = p.clientOrderId;
    if (p.price !== undefined) item["price"] = p.price;
    if (p.quantity !== undefined) item["quantity"] = p.quantity;
    if (p.quoteQty !== undefined) item["quote_qty"] = p.quoteQty;
    if (p.expiresAt !== undefined) item["expires_at"] = p.expiresAt;
    return item;
  });

  const raw = await transport.request<unknown>("POST", `${ORDERS_BASE}/`, {
    body,
    token,
  });
  return parseBatchCreateOrderResponse(raw);
}

export async function cancelOrders(
  transport: Transport,
  token: string,
  orderIds: string[],
): Promise<BatchCancelOrderResponse> {
  validateBatchCancelOrderIds(orderIds);

  const raw = await transport.request<unknown>("DELETE", `${ORDERS_BASE}/`, {
    body: { order_ids: orderIds },
    token,
  });
  return parseBatchCancelOrderResponse(raw);
}
