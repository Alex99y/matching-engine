# ui

React web frontend for visualizing the matching engine's order book and candle charts live.

## Run locally

Requires the `api` (and `core`) to already be running — see the root [README](../README.md#local-development).

```sh
npm install
npm run dev
```

Then open http://localhost:5173.

## Run via Docker

From the repo root:

```sh
docker compose -f local-deploy/docker-compose.yml up -d ui
```
