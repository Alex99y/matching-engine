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
  CreateTokenResult,
  GetOrdersFilter,
  GetUserOperationsFilter,
  Operation,
  Order,
  RefreshSessionResult,
  Session,
  SessionScope,
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
   * @throws {@link AuthenticationError} (403) when called from a read-scoped session.
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
   * @throws {@link AuthenticationError} (403) when called from a read-scoped session.
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
   * List the authenticated user's deposit/withdraw/freeze/unfreeze history,
   * newest first. These rows are only ever written by an admin via the CLI
   * (`cli user balance add|remove|freeze|unfreeze`) — there is no way to
   * create one through this SDK.
   *
   * @param filter - Optional date range and limit (see {@link GetUserOperationsFilter}).
   * @throws {@link ValidationError} when `limit` is out of 1-100 or dates are malformed.
   * @throws {@link APIError} on server-side failures.
   * @example
   * const recent = await session.getOperations({ limit: 20 });
   * for (const op of recent) console.log(op.type, op.symbol, op.amount);
   */
  async getOperations(filter: GetUserOperationsFilter = {}): Promise<Operation[]> {
    return users.getOperations(this.transport, this.token, filter);
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

  /**
   * List the authenticated user's active (non-expired, non-revoked) sessions
   * — useful for building a "log out other devices" view.
   *
   * @throws {@link AuthenticationError} (401) when the token is invalid or expired.
   * @example
   * const active = await session.getActiveSessions();
   * console.log(active.map((s) => s.sessionId));
   */
  async getActiveSessions(): Promise<Session[]> {
    return sessionsResource.getActiveSessions(this.transport, this.token);
  }

  /**
   * Revoke one of the authenticated user's active sessions by its session id
   * (as returned by {@link getActiveSessions}) — not necessarily the caller's
   * own session. Unlike {@link logout}, which always revokes the session
   * behind the current bearer token, this can end a session on another device.
   * Restricted to login-origin sessions — a minted token can never revoke a
   * session other than itself (use {@link logout} for that).
   *
   * @param sessionId - A session id from {@link getActiveSessions}.
   * @throws {@link ValidationError} when `sessionId` is empty.
   * @throws {@link AuthenticationError} (403) when called from a minted (non-login) session.
   * @throws {@link APIError} (404) when no active session matches `sessionId` for this user.
   * @example
   * await session.revokeSession(otherSessionId);
   */
  async revokeSession(sessionId: string): Promise<void> {
    await sessionsResource.revokeSession(this.transport, this.token, sessionId);
  }

  /**
   * Extend this session's expiry by the server's standard TTL. The bearer
   * token value itself never changes — only its server-side expiry does — so
   * there is nothing to swap on this client after calling it. Subject to an
   * absolute age cap enforced server-side: eventually you must {@link
   * MatchingEngineClient.login} again regardless of how often you refresh.
   *
   * @throws {@link AuthenticationError} (401) when the session can no longer be
   * refreshed (expired, revoked, or past the absolute age cap) — log in again.
   * @example
   * const { expiresAt } = await session.refreshSession();
   */
  async refreshSession(): Promise<RefreshSessionResult> {
    return sessionsResource.refreshSession(this.transport, this.token);
  }

  /**
   * Mint a new token scoped to "read" (cannot trade) or "write" (can trade),
   * for handing to a bot or long-running process instead of sharing this
   * session's own credential. Restricted to login-origin sessions — a minted
   * token can never mint another one. Reload the returned token later with
   * {@link MatchingEngineClient.withToken}.
   *
   * @param scope - {@link SessionScope.Read} or {@link SessionScope.Write}.
   * @throws {@link ValidationError} when `scope` is not "read" or "write".
   * @throws {@link AuthenticationError} (403) when called from a minted (non-login) session.
   * @example
   * const { token } = await session.createToken(SessionScope.Read);
   * // persist `token`; later: client.withToken(token)
   */
  async createToken(scope: SessionScope): Promise<CreateTokenResult> {
    return sessionsResource.createToken(this.transport, this.token, { scope });
  }
}
