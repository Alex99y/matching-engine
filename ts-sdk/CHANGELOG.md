# Changelog

All notable changes to this SDK are documented here. The project adheres to
[Semantic Versioning](https://semver.org/): removing/renaming an export,
changing a return type, or adding a required field is a major bump; new
optional surface is a minor bump.

## [1.0.0] - 2026-06-10

### Added

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
