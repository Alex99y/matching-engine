# Changelog

## [Unreleased]

### Added

- `MatchingEngineClient.getMatches(market, filter?)` — recent matches (trades)
  for a market, newest first (`GET /api/v1/markets/:market/matches`), the REST
  counterpart of `streamMarket`'s `"trade"` message. `filter` accepts
  `startDate`/`endDate` (`YYYY-MM-DD`, end exclusive) and `limit` (1-100,
  defaults to 100 server-side). Deliberately excludes the underlying
  buy/sell order ids (public, unauthenticated endpoint; those belong to two
  different users' orders). Returns `Match[]` (`id`, `price`, `quantity`,
  `takerSide`, `matchTime`). New exported types: `Match`, `GetMatchesFilter`.
- `MatchingEngineClient.getDepth(market, options?)` — one-shot order-book depth
  snapshot (`GET /api/v1/markets/:market/depth`), the REST counterpart of
  `streamMarket`'s first frame for callers that don't want to hold an SSE
  connection open. `options.group` buckets prices the same way as
  `streamMarket`'s `group` option. Returns `MarketDepth` (`market`, `bids`,
  `asks`; each level is a `BookLevel` — `price`/`quantity` as `bigint`).
  New exported types: `MarketDepth`, `GetDepthOptions`.
- `AuthenticatedClient.getOperations(filter?)` — list the authenticated user's
  deposit/withdraw/freeze/unfreeze history, newest first (`GET /api/v1/users/operations`).
  These rows are only ever written by an admin via the CLI
  (`cli user balance add|remove|freeze|unfreeze`); there is no way to create one through
  this SDK. `filter` accepts `startDate`/`endDate` (`YYYY-MM-DD`, end exclusive) and
  `limit` (1-100, defaults to 100 server-side). Returns `Operation[]`.
- New exported types: `Operation` (`id`, `symbol`, `amount`, `type`, `reason?`,
  `createdAt`), `OperationType` const object (`Deposit`, `Withdraw`, `Freeze`,
  `Unfreeze`), `GetUserOperationsFilter`.
- `validateGetUserOperationsFilter` client-side guard (same `limit`/date rules as
  `validateGetOrdersFilter`).
- `AuthenticatedClient.refreshSession()` — extends the current session's expiry by the
  server's standard TTL (`POST /api/v1/sessions/refresh`), up to an absolute age cap
  enforced server-side. The bearer token value never changes, so there is nothing to
  swap on the client afterward. Returns `RefreshSessionResult` (`expiresAt`).
- `AuthenticatedClient.createToken(scope)` — mints a new token scoped to `"read"`
  (cannot trade) or `"write"` (can trade) (`POST /api/v1/sessions/tokens`), for handing
  to a bot or long-running process instead of sharing this session's own credential.
  Restricted to login-origin sessions server-side: throws `AuthenticationError` (403)
  when called from an already-minted token. Returns `CreateTokenResult` (`token`,
  `scope`, `expiresAt`).
- `MatchingEngineClient.withToken(token)` — builds an `AuthenticatedClient` from an
  existing bearer token (e.g. one minted via `createToken` and persisted by the
  caller), skipping the login round trip. Required companion to `createToken`: without
  it a minted token could be created but never reloaded into a working client.
- New exported types: `SessionOrigin` (`"login"` | `"minted"`), `SessionScope`
  (`"read"` | `"write"`), `CreateTokenParams`, `CreateTokenResult`,
  `RefreshSessionResult`.
- `Session` gained `origin`, `scope` (both required — the API always sends them), and
  optional `userAgent`/`ipAddress` (best-effort request metadata captured at session
  creation; absent on sessions created before this field existed).
- `validateCreateTokenParams` client-side guard (`scope` must be `"read"` or `"write"`).

### Changed

- `Balance` gained a required `frozen: bigint` field — the amount an admin has frozen
  via the CLI, excluded from both `balance` (tradeable) and `blocked` (locked in the
  user's own open orders).
- `AuthenticatedClient.revokeSession(sessionId)` now throws `AuthenticationError` (403)
  when called from a minted (non-login) token — a minted token can revoke itself (via
  `logout()`) but never another session.
- `AuthenticatedClient.createOrders()` / `cancelOrders()` now throw
  `AuthenticationError` (403) when called from a read-scoped session.

> **Breaking change:** `Session` gained two required fields (`origin`, `scope`), and
> `Balance` gained a required `frozen` field. Code that constructs a literal
> `Session`- or `Balance`-typed value (not just reads one returned by the API) needs
> updating. Folded into this release's existing major bump (see the
> `createOrder`/`cancelOrder` removal below).

### Added

- `AuthenticatedClient.getActiveSessions()` — list the authenticated user's active
  (non-expired, non-revoked) sessions (`GET /api/v1/sessions/active`). Returns
  `Session[]`, useful for a "log out other devices" view.
- `AuthenticatedClient.revokeSession(sessionId)` — revoke a specific active session by
  its `sessionId` (`DELETE /api/v1/sessions/active`), which may be a different session
  than the one behind the current bearer token (unlike `logout()`). Throws
  `ValidationError` for an empty `sessionId` and `APIError` (404) when no active
  session matches it for this user.
- New exported type: `Session` (`sessionId`, `createdAt`, `expiresAt`).
- `MatchingEngineClient.getCandles(market, params)` — fetch historical OHLCV candles
  (`GET /api/v1/markets/:market/candles`). Returns a `GetCandlesResponse` with an
  array of `Candle` objects; OHLCV amounts are `bigint` (decoded from the API's
  decimal-string wire format). The range `[from, to)` must span at most 1000 candles;
  client-side `ValidationError` is thrown for out-of-range requests before they reach
  the network.
- `MatchingEngineClient.streamCandles(market, interval, options?)` — public SSE stream
  for live candle updates (`GET /api/v1/stream/markets/:market/candles?interval=<sec>`).
  Yields `CandleStreamMessage` events: `CandleSnapshotMessage` (initial forming-bucket
  seed from DB), `CandleTradeMessage` (one per match), and `CandleClosedMessage` (emitted
  when a bucket boundary is crossed).
- New exported types: `Candle`, `GetCandlesParams`, `GetCandlesResponse`,
  `CandleSnapshotMessage`, `CandleTradeMessage`, `CandleClosedMessage`,
  `CandleStreamMessage`, `CandleStreamOptions`.
- `CandleInterval` const object (`OneMinute`, `FiveMinutes`, `FifteenMinutes`,
  `OneHour`, `FourHours`, `OneDay`) with the matching numeric-literal union type.
  Valid values: `60 | 300 | 900 | 3600 | 14400 | 86400`.

### Changed

- `AuthenticatedClient.logout()` is now implemented — calls `DELETE /api/v1/sessions`
  to revoke the session server-side. Previously this was a documented no-op.

### Changed (previous)

- `MatchingEngineClient.login()` now calls `POST /api/v1/sessions` (was
  `POST /api/v1/users/login`). No change to the method signature or return type;
  the route move is fully hidden behind the SDK.

### Added (previous)

- `MatchingEngineClient.streamMarket(market, options?)` — public SSE stream for
  one market (`GET /api/v1/stream/:market`). Yields `StreamMessage` events:
  `SnapshotMessage` (initial full book), `BookMessage` (incremental L2 delta),
  `TradeMessage`, and `HeartbeatMessage`. The optional `group` parameter buckets
  the order book by a multiple of the market's `priceQuantum`.
- `AuthenticatedClient.streamUser(options?)` — private SSE stream for the
  authenticated user (`GET /api/v1/stream/users`). Yields `OrderMessage` events
  covering the full order lifecycle (open → filled/cancelled/rejected).
- New exported types: `StreamMessage`, `SnapshotMessage`, `BookMessage`,
  `TradeMessage`, `HeartbeatMessage`, `OrderMessage`, `BookLevel`,
  `MarketStreamOptions`, `UserStreamOptions`.
- `OrderStatus` const object (`Open`, `Filled`, `PartiallyFilled`, `Cancelled`,
  `Rejected`) exported from the public surface.
- `Transport.streamSSE()` internal method: async generator that handles SSE
  frame parsing, authentication headers, and cancellation via `AbortSignal`.
  Amounts in SSE frames arrive as decimal strings and are parsed losslessly to
  `bigint` without going through `BIGINT_WIRE_FIELDS`.



- `AuthenticatedClient.createOrders(params[])` — submit one or more orders in a
  single request (`POST /api/v1/order/`). Returns `BatchCreateOrderResponse`
  with a per-item result; an item may succeed while others fail validation or
  reference an unknown market. Max 500 orders per call.
- `AuthenticatedClient.cancelOrders(orderIds[])` — request cancellation of one
  or more orders in a single request (`DELETE /api/v1/order/`). Returns
  `BatchCancelOrderResponse` with a per-item result. Max 500 ids per call.
- `BatchCreateOrderResult`, `BatchCreateOrderResponse`, `BatchCancelOrderResult`,
  `BatchCancelOrderResponse` types exported from the public surface.
- `validateBatchCreateOrderParams` and `validateBatchCancelOrderIds` client-side
  guards (fail-fast before hitting the network; checks non-empty, ≤ 500 items,
  and per-item field validity).

### Removed (**breaking**)

- `AuthenticatedClient.createOrder(params)` — replaced by `createOrders`.
- `AuthenticatedClient.cancelOrder(orderId)` — replaced by `cancelOrders`.
- `CreateOrderResult` type — the batch response supersedes it; update any code
  that destructured `{ orderId }` to read `results[n].orderId` instead.

> **Breaking change:** this release requires a major version bump.

### Added

- `AuthenticatedClient.requestFaucetFunds(instrument)` — sandbox/POC self-serve
  funding: credits the authenticated user with a small, fixed amount (1 whole unit)
  of the given instrument (`POST /api/v1/faucet`). Requires a write-scoped session
  server-side. **No rate limit and no lifetime cap** — this is a testing convenience
  only, not something to expose against a real balance sheet; call it repeatedly to
  accumulate balance. Returns `FaucetResult` (`symbol`, `amount`).
- New exported type: `FaucetResult` (`symbol`, `amount`).
- `validateInstrumentSymbol` client-side guard (non-empty instrument symbol).

All notable changes to this SDK are documented here. The project adheres to
[Semantic Versioning](https://semver.org/): removing/renaming an export,
changing a return type, or adding a required field is a major bump; new
optional surface is a minor bump.

## [1.0.0] - 2026-06-10

### Added

- `AuthenticatedClient.cancelOrder(orderId)` — requests cancellation of an open
  order (`DELETE /api/v1/order/:id`). Returns `void` on HTTP 202.
- `MatchingEngineClient(host, port, options)` — public entry point exposing
  `register`, `login`, `getMarkets`, and `getInstruments`.
- `AuthenticatedClient` — returned by `login()`; exposes `getOrder`,
  `getOrders`, `createOrder`, `getBalances`, and a (currently no-op) `logout`.
- `AuthenticatedClient.getBalances()` — fetches all instrument balances for the
  authenticated user (`GET /api/v1/users/balances`). Returns `Balance[]` with
  `name`, `symbol`, `decimals`, `balance` (bigint), and `blocked` (bigint).
- `Balance` type exported from the public SDK surface.
- Full SDK error hierarchy: `SDKError`, `NetworkError`, `TimeoutError`,
  `APIError`, `AuthenticationError`, `RateLimitError`, `ValidationError`,
  `ParseError`.
- bigint-safe (de)serialization for uint64 amount/price fields; `"balance"` and
  `"blocked"` included in `BIGINT_WIRE_FIELDS`.
- Per-request timeout, retries with exponential backoff + jitter (429/5xx),
  client-side input validation, and response-shape validation.

### Fixed

- `GetOrdersFilter.endDate` JSDoc corrected to "exclusive upper bound"
  (`created_at < endDate`); the previous comment incorrectly said "inclusive".
