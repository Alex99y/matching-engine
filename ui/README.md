# ui

React web frontend for visualizing the matching engine's order book and candle charts live.

## Screenshots

**Trading view** — order book, live price ticker, candle chart, and order entry:

![Trading view](images/trading-view-2.png)

**Order history** — click any order for a fill-by-fill breakdown with computed average price:

![Order history detail](images/order-history-detail.png)

## Routes

- `/` — trading view (order book, chart, order entry)
- `/history` — order history and account operations (deposits/withdrawals/freezes), tabbed
- `/faucet` — sandbox faucet to credit test balances

`/history` and `/faucet` require a signed-in session; guests are prompted to sign in.

## Run locally

Requires the `api` (and `core`) to already be running — see the root [README](../README.md#local-development).

```sh
npm install
npm run dev
```

Then open http://localhost:5173.

## Configuration

The API the frontend connects to defaults to `http://localhost:4000` and can be pointed
elsewhere with `VITE_API_URL`:

```sh
VITE_API_URL=https://api.example.com npm run dev
```

Only the **origin** is used — the SDK appends `/api/v1` itself, so a URL carrying a path logs a
warning and has the path ignored. `http://` selects plain HTTP (the SDK's `allowInsecure`) and
`https://` requires TLS; the port defaults to 80 or 443 when the URL omits it. A value that is
not a valid `http(s)` URL logs an error and falls back to the default rather than failing the
page to blank.

Whatever it resolves to only pre-fills the login screen's Host/Port/HTTP fields — they stay
editable, so a wrong value is recoverable without a rebuild.

> `VITE_*` variables are read by Vite at **dev-server start** (or at `npm run build` time for a
> static bundle), never by the browser at runtime. Changing it means restarting `npm run dev`,
> or rebuilding if you are serving `dist/`.

Other settings (book depth, candle history, ticker poll interval) are constants in
[src/config.ts](src/config.ts).

## Run via Docker

From the repo root:

```sh
docker compose -f infra/local-deploy/docker-compose.yml up -d ui
```
