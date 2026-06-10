# Matching Engine TypeScript SDK

A zero-runtime-dependency TypeScript SDK for the matching-engine API, built for
trading bots. ESM-only; works on Node ≥ 22 (uses global `fetch` and
`AbortController`).

## Install & build

```sh
npm install
npm run build      # tsc -> build/
npm test           # vitest
npm run test:coverage
```

## Quick start

```ts
import {
  MatchingEngineClient,
  OrderSide,
  OrderType,
  TimeInForce,
} from "ts-sdk";

// HTTPS is required by default. For a local non-TLS API, pass allowInsecure.
const client = new MatchingEngineClient("api.exchange.com", 443);

// Public, unauthenticated
await client.register({ username: "bot1", email: "bot@x.io", password: "supersecret" });
const markets = await client.getMarkets();
const instruments = await client.getInstruments();

// login() returns the authenticated session client
const session = await client.login({ username: "bot1", password: "supersecret" });

const { orderId } = await session.createOrder({
  market: "ETH-USDT",
  side: OrderSide.Buy,
  type: OrderType.Limit,
  timeInForce: TimeInForce.GoodTillCancel,
  price: 2_000_000n,   // uint64 amounts are bigint (precision-safe)
  quantity: 5n,
});

const order = await session.getOrder(orderId);
const open = await session.getOrders({ market: "ETH-USDT", showOpen: true });
await session.logout();   // no-op until the API exposes a logout endpoint
```

## Notes

- **Amounts are `bigint`.** All uint64 price/quantity/quantum fields are
  exposed and sent as `bigint` to avoid the precision loss `number` suffers
  above 2^53.
- **Local development:** pass `{ allowInsecure: true }` to use `http://`.
  HTTPS is the default and required otherwise.
- **Errors:** every failure is an instance of `SDKError` (or a subclass such as
  `APIError`, `AuthenticationError`, `RateLimitError`, `ValidationError`,
  `ParseError`). Native `fetch`/parse errors never escape.
- **Resilience:** configurable `timeoutMs`, `maxRetries`, and
  `baseRetryDelayMs`; 429/5xx and network errors are retried with backoff.
