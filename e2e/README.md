# e2e

End-to-end tests. They drive a **running** matching-engine stack (api + core + db +
rabbitmq) through the public HTTP/SSE contract only — no `internal/` imports, same view an
external client has.

- **Not run in CI** — CI compiles this module and runs only the stack-free unit tests.

## Layout

| path | status | what |
|---|---|---|
| `internal/config` | done | `E2E_*` env → `Config` |
| `internal/client` | done | black-box REST client (auth, sessions, users, faucet, orders, markets, matches, candles, instruments) |
| `internal/stream` | done | SSE reader + user order stream (`WaitForStatus`) |
| `internal/harness` | done | `WaitReady` / `RequireMarket`, `NewAccount` + `Fund`, `ResolveMarket` |
| `internal/fixtures` | done | order builders (tick/lot-aligned), `ToRaw`/`FromRaw` |
| `internal/assert` | done | `Eventually`, balance conservation, depth, order/stream matchers |
| `tests/{auth,orders,marketdata,streams,recovery}` | — | the suites, behind `//go:build e2e` |

## Run locally

The suite brings its own stack — Postgres, RabbitMQ, migrations, seed data, `core` and `api`,
all in containers. This is the same path CI takes.

```sh
make -C e2e stack-up      # vendors deps, builds the images, waits for the stack
make -C e2e test-e2e      # 51 tests, ~17s
make -C e2e stack-down    # tears it down, volumes included

make -C e2e stack-logs    # service logs, when something fails inside core or api
```

Against a stack you are already running yourself (`make stack-up` at the repo root plus
`core`/`api` on the host), skip `stack-up` and point the suite at it:

```sh
E2E_MARKET=BTC-USDT make -C e2e test-e2e
go test -tags e2e -run TestLimitOrderRestsThenCancels ./tests/orders/...
```

> `E2E_MARKET` must name a market `core` actually serves (its `MARKET_LIST`). The API accepts
> orders for any market in the database, but only served markets have a matcher — orders to
> the rest are accepted and then queue forever.
>
> The e2e stack binds 5432/5672/4000, so it cannot run alongside `infra/local-deploy`.

## Make targets

| target      | what |
|-------------|------|
| `build`      | compile the placeholder `cmd/` binary |
| `test`       | stack-free unit tests, then compile the tagged suite without running it |
| `stack-up`   | vendor deps, build images, bring up the full stack |
| `stack-down` | tear the stack down, volumes included |
| `stack-logs` | service logs from the stack |
| `test-e2e`   | run the suite against a live stack (`test-e2e-v` for per-test output) |
| `clean`      | remove `bin/` |

## Configuration

All via `E2E_*` env (defaults in `internal/config`): `E2E_API_URL`, `E2E_MARKET`,
`E2E_MARKETS`, `E2E_READY_TIMEOUT`, `E2E_SETTLE_TIMEOUT`, `E2E_LOG_LEVEL`, plus
`E2E_CLI_BIN` / `E2E_POSTGRES_URL` for the account freeze, which has no REST route.

Seeding is not the suite's job — it has to happen before `api` starts, since the API caches
its market set at boot. `stack-up` handles it; the suite only verifies (`suite.Setup` →
`RequireMarket`).
