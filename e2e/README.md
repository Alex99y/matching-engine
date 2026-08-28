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

```sh
# 1. bring up infra + migrate + seed defaults + run core & api (see repo README)
docker compose -f ../infra/local-deploy/docker-compose.yml up -d
make -C ../db migrate
# seed ETH/BTC/USDT instruments + ETH-USDT/BTC-USDT/ETH-BTC markets (seed-defaults)
make -C ../core run   # separate shell
make -C ../api run    # separate shell

# 2. run the suite against it
make test-e2e                                  # all suites, defaults target localhost:4000
E2E_API_URL=http://localhost:4000/api/v1 make test-e2e
go test -tags e2e -run TestOrderLifecycle ./tests/orders/...
```

## Make targets

| target      | what |
|-------------|------|
| `build`     | compile the placeholder `cmd/` binary |
| `test`      | stack-free unit tests only (config, client wiring) — the CI check |
| `test-e2e`  | run the full stack-driven suite against a live stack |
| `clean`     | remove `bin/` |

## Configuration

All via `E2E_*` env (defaults in `internal/config`): `E2E_API_URL`, `E2E_MARKET`,
`E2E_MARKETS`, `E2E_READY_TIMEOUT`, `E2E_SETTLE_TIMEOUT`, `E2E_LOG_LEVEL`.
The stack must be **seeded before the API starts**
(the API caches markets at boot) — that is `make stack-up` / CI, not the suite.
