## Liquidity bots

A simple market-making bot that mirrors a live order book from a source exchange (Binance or
OKX) into a market on the ME, placing and updating passive buy/sell orders to match it. It's
meant for testing the ME's order book and matching behavior with realistic depth — not for
production use.

## Running the bots

### Prerequisites

- Node.js ≥ 22 (uses the native `WebSocket` global)
- `ts-sdk` built once: `cd ../ts-sdk && npm install && npm run build`
- A running ME instance with the target market already created, and a user account to trade as

### Setup

```sh
npm install
npm run build
cp .env.example .env   # then fill in the values below
npm start
```

At minimum, `.env` needs the ME connection (`ME_HOST`, `ME_PORT`, `ME_USERNAME`, `ME_PASSWORD`)
and the market to mirror into (`ME_MARKET`, e.g. `BTC-USDT`). Everything else has a sensible
default — see `.env.example` for the full list with comments.

### Choosing a source

Set `PROVIDER` to pick which exchange the bot mirrors:

- `PROVIDER=binance` (default) — also set `BINANCE_SYMBOL` (e.g. `btcusdt`)
- `PROVIDER=okx` — also set `OKX_INST_ID` (e.g. `BTC-USDT`)

### Dry run

Set `BOT_DRY_RUN=true` to log what the bot *would* place/cancel without sending anything to the
ME — useful for checking a config before it touches real orders.
