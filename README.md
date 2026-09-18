# matching-engine

Matching Engine is a proof of concept for an order matching engine built in Go, with durability as its first concern rather than raw speed.

Each market's book is held in memory for matching, but **PostgreSQL is the source of truth**. A batch of orders is matched in memory and every side effect — trades, balances, order state — is written in a single transaction; only once that commits are the broker messages acknowledged. A crash therefore replays work rather than losing it, and the book is rebuilt from the database on restart. Orders reach the engine over **RabbitMQ** (one queue per market), and live market data is fanned back out through a separate exchange to SSE subscribers.

That design sets the pace: one matcher goroutine owns each market and commits serially, which tops out around 2,500 orders/s per market with tail latency degrading from roughly 1,500 orders/s upward — far below a memory-only engine, and a deliberate trade rather than an accident. See [loadtest/README.md](loadtest/README.md) for the measurements.

The engine supports limit and market orders, various time-in-force options, and is optimized for concurrent processing.


## Project structure

- `core` - core logic of the matching engine, including order processing and matching algorithms
- `api` - API endpoints for interacting with the matching engine
- `db` - database models, migrations and repositories
- `common` - shared utilities and types used across Go services, and the protobuf wire schema (`common/proto`)
- `bots` - Node.js bots for testing and simulating order flow against the engine
- `ts-sdk` - TypeScript SDK for the API, used by trading bots to interact with the engine
- `ui` - React web frontend for visualizing the matching engine's order book and candle charts live
- `infra` - deployment: `local-deploy` (Docker Compose for local dev + `docker-compose-deps.yml` for the e2e stack) and `gcp-deploy`
- `e2e` - end-to-end tests against a running stack (see `e2e/PLAN.md`)
- `loadtest` - Go load-testing suite measuring order ack/match/cancel latency under configurable background load (see `loadtest/README.md`)

## Software Requirements

- Go (>= 1.25.7)
- Postgresql (>= 18.4)
- Rabbitmq (>= 4.2.4)
- protoc (>= 30) — only to change the wire schema; the generated Go code is committed

## Build

```sh
make build
```

## Wire schema

Messages between `api` and `core` travel over RabbitMQ as Protocol Buffers. The schema lives in
`common/proto` and the generated Go code (`common/pkg/pb`) is committed, so a plain build needs no
protobuf tooling. After editing a `.proto`:

```sh
make proto
```

This installs `protoc-gen-go` at the version pinned in the `Makefile` (it must match the
`google.golang.org/protobuf` the modules depend on) and regenerates `common/pkg/pb`. No `.proto`
names a Go import path: each file's package follows from its directory
(`common/proto/me/v1` → `common/pkg/pb/me/v1`, package `mev1`), so a new schema version is just a
new directory. Commit the regenerated files, and re-run `go work vendor` before building the Docker
images. Design notes in [core/ARCHITECTURE.md](core/ARCHITECTURE.md) § Wire format.

## Docker

```sh
# Vendor dependencies first (only needed once, or after dependency changes)
go work vendor

# Build images
docker build -f api/Dockerfile  -t matching-engine/api  .
docker build -f core/Dockerfile -t matching-engine/core .
docker build -f db/Dockerfile   -t matching-engine/db   .
```

## Local Development

Bring the pieces up in this order: infrastructure, database migrations, `core`, then `api`. `ui` and `bots` are optional clients on top.

> **Shortcut:** `make stack-up` does steps 1–2 plus seeds the default instruments/markets
> (Postgres + RabbitMQ only, via `infra/local-deploy/docker-compose-deps.yml`). Then start
> `core` and `api` (steps 3–4). `make stack-down` tears the deps down. For the full local
> stack incl. Prometheus/Grafana/UI, use the compose file below instead.

### 1. Infrastructure

Start Postgres, RabbitMQ, Prometheus, and Grafana:

```sh
docker compose -f infra/local-deploy/docker-compose.yml up -d
```

### 2. Database migrations

```sh
make -C db migrate
```

### 3. Core (matching engine)

> `core` needs to be configured (its own environment file) before it will run. Besides the database
> and broker URLs it requires `ADMIN_PORT` and `ADMIN_TOKEN` — core refuses to start without a token
> rather than exposing its admin API anonymously. See `core/.env`.

```sh
make -C core run
```

Trading can be halted and restarted per market without stopping the process:

```sh
export CORE_ADMIN_URL=http://localhost:9093 ADMIN_TOKEN=<the token from core/.env>
cli market status
cli market pause  --market BTC-USDT   # orders queue in the broker; nothing is rejected or lost
cli market resume --market BTC-USDT   # the backlog drains in arrival order
```

While a market is paused its cancels are held too, so resting orders cannot be withdrawn and their
funds stay blocked. The halt lives in the running process: restarting core resumes every market.

The same admin API cleans up after a frozen account — freezing stops new orders, but whatever the
user already has resting stays in the book with their funds blocked:

```sh
cli user freeze        --username alice     # required first; the cancel refuses otherwise
cli user orders        --username alice
cli user cancel-orders --username alice --all
```

### 4. API

> `api` needs to be configured (its own environment file) before it will run.

```sh
make -C api run
```

### 5. (Optional) UI

Either as a container:

```sh
docker compose -f infra/local-deploy/docker-compose.yml up -d ui
```

or directly on the host:

```sh
cd ui && npm install && npm run dev
```

### 6. (Optional) Bots

```sh
cd bots && npm install && npm run build && npm start
```

> `bots` also needs to be configured (its own environment variables) before it will run.

### 7. (Optional) Load testing

```sh
make -C loadtest run-ack LEVEL=1
```

> See [docs/load-testing.md](docs/load-testing.md) for the env var reference and results.