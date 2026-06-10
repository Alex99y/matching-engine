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
  `getOrders`, `createOrder`, and a (currently no-op) `logout`.
- Full SDK error hierarchy: `SDKError`, `NetworkError`, `TimeoutError`,
  `APIError`, `AuthenticationError`, `RateLimitError`, `ValidationError`,
  `ParseError`.
- bigint-safe (de)serialization for uint64 amount/price fields.
- Per-request timeout, retries with exponential backoff + jitter (429/5xx),
  client-side input validation, and response-shape validation.
