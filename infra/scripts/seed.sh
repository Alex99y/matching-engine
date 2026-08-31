#!/bin/sh
# Seeds the project's default instruments and markets. One source of truth for both callers:
# `make stack-seed` (local development, against the host cli binary) and the e2e stack's
# one-shot seed service.
#
# It must run before core and api start: core only launches matchers for the markets named in
# MARKET_LIST, and the API caches its market set at boot, so anything seeded afterwards is
# invisible to both.
#
# CLI overrides the binary path — the e2e image ships it at /bin/cli, a host run points at
# ./cli/bin/cli. POSTGRESQL_URL must be set by the caller.
#
# Idempotent: the CLI exits non-zero when an item already exists, which is a normal outcome
# on a reused volume, so those are tolerated. A genuine failure surfaces on the readback at
# the end, which is what the exit code reflects.
set -eu

CLI=${CLI:-/bin/cli}

echo "==> seeding instruments"
"$CLI" instrument create --json '[
  {"name":"Ethereum","symbol":"ETH","decimals":18},
  {"name":"Bitcoin","symbol":"BTC","decimals":9},
  {"name":"Tether","symbol":"USDT","decimals":6}
]' || echo "   (one or more already existed)"

echo "==> seeding markets"
"$CLI" market create --json '[
  {"name":"ETH-USDT","price_quantum":1,"amount_quantum":1000000000000000,"min_order_size":1000000000000000,"max_order_size":1000000000000000000,"taker_fee_bps":100,"maker_fee_bps":50},
  {"name":"BTC-USDT","price_quantum":1,"amount_quantum":1000000,"min_order_size":1000000,"max_order_size":1000000000000000000,"taker_fee_bps":100,"maker_fee_bps":50},
  {"name":"ETH-BTC","price_quantum":1,"amount_quantum":1000000000000000,"min_order_size":1000000000000000,"max_order_size":1000000000000000000,"taker_fee_bps":100,"maker_fee_bps":50}
]' || echo "   (one or more already existed)"

# Prove the seed actually landed rather than trusting the tolerated exit codes above: core
# and api are about to start against this data, and a silent miss here shows up much later as
# an unexplained "market not found".
echo "==> verifying"
for market in ETH-USDT BTC-USDT ETH-BTC; do
	# `market get` prints a header row and then one row per match, each starting with the
	# market's numeric id — so a leading digit is what distinguishes "found" from "header only".
	if ! "$CLI" market get --name "$market" | grep -qE '^[0-9]+[[:space:]]'; then
		echo "seed failed: market $market is missing" >&2
		exit 1
	fi
done

echo "==> seed complete"
