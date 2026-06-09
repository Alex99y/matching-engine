---
name: seed-defaults
description: >
  Create the default instruments (ETH, BTC, USDT) and markets (ETH-USDT, BTC-USDT, ETH-BTC)
  in the matching engine database with a single command. No input required beyond the DB URL.
  Trigger on phrases like "seed the database", "create default instruments and markets",
  "bootstrap the engine", or "set up default trading pairs".
---

# Seed Defaults Skill

Create the standard set of instruments and markets in one shot. No interactive input is needed
beyond the database URL.

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

## Step 3 — Create default instruments

Run a single CLI call with all three instruments:

```bash
POSTGRESQL_URL=<url> ./cli/bin/cli instrument create --json '[
  {"name": "Ethereum", "symbol": "ETH",  "decimals": 18},
  {"name": "Bitcoin",  "symbol": "BTC",  "decimals": 9},
  {"name": "Tether",   "symbol": "USDT", "decimals": 6}
]'
```

If any instrument already exists the CLI will report it and continue — this is not a fatal error.
Only stop if an unexpected error occurs.

---

## Step 4 — Create default markets

Run a single CLI call with all three markets:

```bash
POSTGRESQL_URL=<url> ./cli/bin/cli market create --json '[
  {"name": "ETH-USDT", "price_quantum": 1, "amount_quantum": 1000000000000000,  "min_order_size": 1000000000000000,  "max_order_size": 1000000000000000000},
  {"name": "BTC-USDT", "price_quantum": 1, "amount_quantum": 1000000,           "min_order_size": 1000000,           "max_order_size": 1000000000000000000},
  {"name": "ETH-BTC",  "price_quantum": 1, "amount_quantum": 1000000000000000,  "min_order_size": 1000000000000000,  "max_order_size": 1000000000000000000}
]'
```

> **Note on quantum values:** the defaults above are reasonable starting points based on each
> token's decimal precision (ETH: 18, BTC: 9, USDT: 6), but the user may want different values
> for their use case. If the user provided explicit quantum/size values before invoking this skill,
> use those instead.

If any market already exists the CLI will report it and continue — this is not a fatal error.
Only stop if an unexpected error occurs.

---

## Step 5 — Report the result

Print a summary of what was created and what was skipped (already existed), e.g.:

```
Instruments:
  ✓ ETH — created
  ✓ BTC — created
  ✓ USDT — already existed (skipped)

Markets:
  ✓ ETH-USDT — created
  ✓ BTC-USDT — created
  ✓ ETH-BTC  — created
```

If all three instruments and all three markets are either freshly created or already existed,
the seed is considered successful.
