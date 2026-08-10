# matching-engine

Matching Engine project is a proof of concept for a high-performance order matching engine built in Go, designed to handle a large volume of orders with low latency. The engine supports limit and market orders, various time-in-force options, and is optimized for concurrent processing.


## Project structure

- `core` - core logic of the matching engine, including order processing and matching algorithms
- `api` - API endpoints for interacting with the matching engine
- `db` - database models, migrations and repositories
- `common` - shared utilities and types used across Go services
- `bots` - Node.js bots for testing and simulating order flow against the engine
- `ts-sdk` - TypeScript SDK for the API, used by trading bots to interact with the engine
- `ui` - React web frontend for visualizing the matching engine's order book and candle charts live
- `local-deploy` - Docker and local deployment scripts

## Software Requirements

- Go (>= 1.25.7)
- Postgresql (>= 18.4)
- Rabbitmq (>= 4.2.4)

## Build

```sh
make build
```

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

### 1. Infrastructure

Start Postgres, RabbitMQ, Prometheus, and Grafana:

```sh
docker compose -f local-deploy/docker-compose.yml up -d
```

### 2. Database migrations

```sh
make -C db migrate
```

### 3. Core (matching engine)

> `core` needs to be configured (its own environment file) before it will run.

```sh
make -C core run
```

### 4. API

> `api` needs to be configured (its own environment file) before it will run.

```sh
make -C api run
```

### 5. (Optional) UI

Either as a container:

```sh
docker compose -f local-deploy/docker-compose.yml up -d ui
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