# Changelog

## [Unreleased]

### Added

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
