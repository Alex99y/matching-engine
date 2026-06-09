---
name: create-instrument
description: >
  Interactively create one or more instruments in the matching engine database by gathering
  the required fields from the user and running the CLI binary. Trigger on phrases like
  "create an instrument", "add instrument", "new instrument", or "register a token".
---

# Create Instrument Skill

Guide the user through creating one or more instruments, then execute the CLI to persist them.

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

## Step 3 — Gather instrument fields

Ask the user for the following. Collect all answers before running anything.

| Field | Description | Validation |
|-------|-------------|------------|
| `name` | Full name of the instrument | Required, max 100 chars |
| `symbol` | Ticker symbol | Required, max 10 chars — auto-uppercase |
| `decimals` | Decimal precision | Integer 0–18, default 0 |

If the user wants to create **multiple instruments at once**, tell them they can provide a JSON
array and you will use the `--json` flag instead.

---

## Step 4 — Run the CLI

For a **single instrument**:

```bash
POSTGRESQL_URL=<url> ./cli/bin/cli instrument create \
  --name "<name>" \
  --symbol "<SYMBOL>" \
  --decimals <decimals>
```

For **multiple instruments via JSON**:

Build the JSON array from the user's answers, then run:

```bash
POSTGRESQL_URL=<url> ./cli/bin/cli instrument create \
  --json '[{"name":"Bitcoin","symbol":"BTC","decimals":8},{"name":"Tether","symbol":"USDT","decimals":6},{"name":"Ethereum","symbol":"ETH","decimals":18}]'
```

Each object in the array must have `name` (string), `symbol` (string), and optionally `decimals`
(integer, defaults to `0` if omitted). Symbols are auto-uppercased by the CLI.

---

## Step 5 — Report the result

- On success (`exit 0`): confirm each instrument was created, e.g. `created instrument BTC`.
- On failure: show the error message from the CLI output and explain the cause if possible
  (e.g. "instrument already exists", "symbol too long").
- Do **not** suggest retrying with different data unless the user asks.
