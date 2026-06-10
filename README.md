# matching-engine

Matching Engine project is a proof of concept for a high-performance order matching engine built in Go, designed to handle a large volume of orders with low latency. The engine supports limit and market orders, various time-in-force options, and is optimized for concurrent processing.


## Project structure

- `core` - core logic of the matching engine, including order processing and matching algorithms
- `api` - API endpoints for interacting with the matching engine
- `db` - database models, migrations and repositories
- `common` - shared utilities and types used across Go services
- `bots` - Node.js bots for testing and simulating order flow against the engine
- `ts-sdk` - TypeScript SDK for the API, used by trading bots to interact with the engine
- `local-deploy` - Docker and local deployment scripts

## Software Requirements

- Go (>= 1.25.7)
- Postgresql (>= 18.4)
- Rabbitmq (>= 4.2.4)