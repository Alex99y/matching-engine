import type { Transport } from "../http/transport.js";
import * as orders from "../resources/orders.js";
import * as sessionsResource from "../resources/sessions.js";
import * as streamResource from "../resources/stream.js";
import * as users from "../resources/users.js";
import type {
  Balance,
  BatchCancelOrderResponse,
  BatchCreateOrderResponse,
  CreateOrderParams,
  GetOrdersFilter,
  Order,
  StreamMessage,
  UserStreamOptions,
} from "../types/index.js";

/**
 * Authenticated session client. Obtained from
 * {@link MatchingEngineClient.login} and carrying the bearer token used for
 * every private call. Construct two of these with different tokens and they
 * stay fully independent — no shared mutable state.
 */
export class AuthenticatedClient {
  private readonly transport: Transport;
  private readonly token: string;

  constructor(transport: Transport, token: string) {
    this.transport = transport;
    this.token = token;
  }

  /** The bearer token backing this session. */
  get authToken(): string {
    return this.token;
  }

  /**
   * Fetch a single order owned by the authenticated user.
   *
   * @param orderId - The order's UUID.
   * @throws {@link ValidationError} when `orderId` is empty.
   * @throws {@link APIError} (404) when the order does not exist.
   * @example
   * const order = await session.getOrder("0190f...");
   */
  async getOrder(orderId: string): Promise<Order> {
    return orders.getOrder(this.transport, this.token, orderId);
  }

  /**
   * List the authenticated user's orders. With no filter the API returns the
   * 10 most recent. Passing `clientOrderId` returns at most one order.
   *
   * @param filter - Optional market/date/status filters (see {@link GetOrdersFilter}).
   * @throws {@link ValidationError} when `limit` is out of 1-100 or dates are malformed.
   * @throws {@link APIError} on server-side failures.
   * @example
   * const open = await session.getOrders({ market: "ETH-USDT", showOpen: true });
   */
  async getOrders(filter: GetOrdersFilter = {}): Promise<Order[]> {
    return orders.getOrders(this.transport, this.token, filter);
  }

  /**
   * Submit one or more orders in a single request (max 500). The API accepts
   * each item asynchronously (HTTP 202) and returns a per-item result; an item
   * may succeed while another fails validation or references an unknown market.
   * Check `result.error` on each item to detect partial failures.
   *
   * @param params - Array of order parameters (1–500 items). Each item accepts
   *   `clientOrderId` for per-order idempotency.
   * @throws {@link ValidationError} on an empty array, batch size > 500, or
   *   invalid side/type/tif on any item.
   * @throws {@link APIError} on server-side failures.
   * @example
   * const { results } = await session.createOrders([
   *   {
   *     market: "ETH-USDT",
   *     side: OrderSide.Buy,
   *     type: OrderType.Limit,
   *     timeInForce: TimeInForce.GoodTillCancel,
   *     price: 2_000_000n,
   *     quantity: 5n,
   *   },
   * ]);
   * for (const r of results) {
   *   if (r.error) console.error(`index ${r.index}: ${r.error}`);
   *   else console.log(`index ${r.index}: orderId=${r.orderId}`);
   * }
   */
  async createOrders(params: CreateOrderParams[]): Promise<BatchCreateOrderResponse> {
    return orders.createOrders(this.transport, this.token, params);
  }

  /**
   * Request cancellation of one or more open orders in a single request
   * (max 500). The API accepts each cancel asynchronously (HTTP 202); orders
   * are removed from the book out of band. Partial failures are reported
   * per-item — check `result.error` for items that could not be cancelled.
   *
   * @param orderIds - Array of order UUIDs to cancel (1–500 items).
   * @throws {@link ValidationError} on an empty array, batch size > 500, or
   *   an empty string in the array.
   * @throws {@link APIError} on server-side failures.
   * @example
   * const { results } = await session.cancelOrders(["0190f...", "0190g..."]);
   * for (const r of results) {
   *   if (r.error) console.error(`${r.orderId}: ${r.error}`);
   * }
   */
  async cancelOrders(orderIds: string[]): Promise<BatchCancelOrderResponse> {
    return orders.cancelOrders(this.transport, this.token, orderIds);
  }

  /**
   * Fetch all instrument balances for the authenticated user.
   *
   * @throws {@link APIError} on server-side failures.
   * @example
   * const balances = await session.getBalances();
   * console.log(balances[0]?.symbol, balances[0]?.balance);
   */
  async getBalances(): Promise<Balance[]> {
    return users.getBalances(this.transport, this.token);
  }

  /**
   * Open a private SSE stream for the authenticated user, yielding
   * {@link OrderMessage} events for every order lifecycle change (fill,
   * cancellation, rejection). Break out of the loop or abort
   * `options.signal` to close the connection.
   *
   * @param options - Optional cancellation signal.
   * @throws {@link AuthenticationError} on an invalid or expired token.
   * @throws {@link NetworkError} on connection failure.
   * @example
   * for await (const msg of session.streamUser()) {
   *   if (msg.type === "order") console.log(msg.orderId, msg.status);
   * }
   */
  streamUser(
    options: UserStreamOptions = {},
  ): AsyncGenerator<StreamMessage, void, undefined> {
    return streamResource.streamUser(this.transport, this.token, options);
  }

  /**
   * Invalidate the current session. After this call the bearer token is revoked
   * server-side and any further authenticated request will return 401.
   *
   * @throws {@link AuthenticationError} (401) when the token is already expired or revoked.
   * @example
   * await session.logout();
   */
  async logout(): Promise<void> {
    await sessionsResource.logout(this.transport, this.token);
  }
}
