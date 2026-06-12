---
name: create-market
description: >
  Interactively create one or more markets in the matching engine database by gathering the
  required fields from the user and running the CLI binary. Trigger on phrases like
  "create a market", "add market", "new market", or "open a trading pair".
---

# Create Market Skill

Guide the user through creating one or more markets, then execute the CLI to persist them.

---

## Step 1 — Resolve the DB URL

Run this command to check if `POSTGRESQL_URL` is already stored in `cli/.env`:

```bash
grep -s "^POSTGRESQL_URL=" cli/.env
```

- If the variable is found, extract its value and use it silently (do not print it to the user).
- If it is **not** found, ask the user:

> "What is the PostgreSQL connection URL? (e.g. `postgres://user:password@localhost:5432/dbname`)"

Once you have the URL, offer to save it so the user does not need to re-enter it next time:

> "Save this URL to `cli/.env` for future use? (y/n)"

If yes, write or append `POSTGRESQL_URL=<url>` to `cli/.env`.

---

## Step 2 — Ensure the binary is built

Check whether `cli/bin/cli` exists:

```bash
test -f cli/bin/cli && echo "exists"
```

If it does **not** exist, build it before proceeding:

```bash
make -C cli build
```

---

## Step 3 — Gather market fields

Ask the user for the following. Collect all answers before running anything.

| Field | Description | Validation |
|-------|-------------|------------|
| `name` | Trading pair in `BASE-QUOTE` format (e.g. `BTC-USDT`) | Required — both instruments must already exist in the DB |
| `price_quantum` | Minimum price increment (integer, in atomic units) | Must be > 0 |
| `amount_quantum` | Minimum amount increment (integer, in atomic units) | Must be > 0 |
| `min_order_size` | Minimum order size | Must be > 0 and a multiple of `amount_quantum` |
| `max_order_size` | Maximum order size | Must be ≥ `min_order_size` and a multiple of `amount_quantum` |
| `taker_fee_bps` | Taker fee in basis points (1 bp = 0.01%) — **optional** | 0–10000; defaults to 0 if omitted |
| `maker_fee_bps` | Maker fee in basis points (1 bp = 0.01%) — **optional** | 0–10000; defaults to 0 if omitted |

**Fees are optional.** If the user does not mention fees, omit the flags and they default to 0.
A common starting point is **100 bps taker (1%) and 50 bps maker (0.5%)**; suggest these if the
user wants fees but is unsure. Fees are charged at match time on the asset each side receives.

**Explain quantums to the user if they seem unsure:**
> "All values are in atomic (integer) units. For example, if BTC has 8 decimals, `1 BTC` = `100000000` units. A `price_quantum` of `1` means prices can be incremented by the smallest representable unit."

**Key constraint to surface before asking:**
> "Both instruments (`BASE` and `QUOTE`) must already exist. If they don't, create them first with `/create-instrument`."

If the user wants to create **multiple markets at once**, tell them they can provide a JSON
array and you will use the `--json` flag instead.

---

## Step 4 — Run the CLI

For a **single market**:

```bash
POSTGRESQL_URL=<url> ./cli/bin/cli market create \
  --name "<BASE-QUOTE>" \
  --price_quantum <value> \
  --amount_quantum <value> \
  --min_order_size <value> \
  --max_order_size <value> \
  --taker_fee_bps 100 \
  --maker_fee_bps 50
```

The `--taker_fee_bps` and `--maker_fee_bps` flags are optional; omit them to default to 0.

For **multiple markets via JSON**:

Build the JSON array from the user's answers, then run:

```bash
POSTGRESQL_URL=<url> ./cli/bin/cli market create \
  --json '[{"name":"BTC-USDT","price_quantum":1,"amount_quantum":1000,"min_order_size":1000,"max_order_size":1000000000,"taker_fee_bps":100,"maker_fee_bps":50},{"name":"ETH-USDT","price_quantum":1,"amount_quantum":100,"min_order_size":100,"max_order_size":500000000,"taker_fee_bps":100,"maker_fee_bps":50}]'
```

Each object in the array must have the five required fields: `name`, `price_quantum`,
`amount_quantum`, `min_order_size`, and `max_order_size`. The fee fields `taker_fee_bps` and
`maker_fee_bps` are **optional** (default 0 when omitted). All quantum and size values are
integers in atomic units; fees are integers in basis points (0–10000). The same validation rules
apply per entry — the CLI will report each failure individually and continue with the rest.

---

## Step 5 — Report the result

- On success (`exit 0`): confirm each market was created, e.g. `created market BTC-USDT`.
- On failure: show the error message and explain the cause if possible:
  - `"instrument BTC or USDT not found"` → instruments need to be created first.
  - `"market already exists"` → this trading pair is already registered.
  - Validation errors (e.g. `min_order_size` not a multiple of `amount_quantum`) → show the
    failing constraint and the corrected values the user should use.
- Do **not** suggest retrying with different data unless the user asks.
