# Load Testing

The `loadtest` module measures matching-engine latency from a client's perspective (HTTP + SSE only,
no internal imports), under configurable background load.

| Binary | Measures |
|--------|----------|
| `latency-ack` | order sent → received by the ME (first stream event of any kind) |
| `latency-match` | order sent → first matched (`filled`/`partially_filled`) |
| `latency-cancel` | cancel sent → cancelled, and order sent → cancelled (two spans) |

## Running it

```sh
make -C loadtest run-ack LEVEL=2
make -C loadtest run-match LEVEL=0
make -C loadtest run-cancel LEVEL=3
```

`LOADTEST_LEVEL` (0-3) is the only required env var. Others: `LOADTEST_API_URL`, `LOADTEST_MARKET` (default
`ETH-USDT`), `LOADTEST_DURATION`, `LOADTEST_SAMPLE_COUNT`, `LOADTEST_WARMUP`, `LOADTEST_MAKER_ACCOUNTS`/
`LOADTEST_TAKER_ACCOUNTS`, `LOADTEST_OUTPUT_DIR`.

| Level | Spam rate | Duration | Samples |
|-------|-----------|----------|---------|
| 0 idle | 0 op/s | 30s | 100 |
| 1 low | 100 op/s | 60s | 200 |
| 2 medium | 600 op/s | 60s | 200 |
| 3 high | 1500 op/s | 90s | 200 |

⚠️ `LOADTEST_MARKET` must be in `core`'s `MARKET_LIST` or orders queue forever with no consumer. All
runs below used `LOADTEST_MARKET=BTC-USDT`.

## Results (BTC-USDT, local dev: single api + single core)

Test machine: AMD Ryzen 7 PRO 5850U (8c/16t), 64 GB RAM, NVMe SSD, Ubuntu OS 24.04. All services
(`api`, `core`, Postgres, RabbitMQ, and the load-test client) ran on this one machine — no network
hops between them.

### `latency-ack`

| level | spam target/achieved | n | dead/timeout | min | p50 | p90 | p95 | p99 | max |
|---|---|---|---|---|---|---|---|---|---|
| 0 | 0 / 0.0 | 100 | 0/0 | 10.14ms | 16.54ms | 18.79ms | 19.18ms | 20.21ms | 22.78ms |
| 1 | 100 / 100.1 | 200 | 0/0 | 7.91ms | 10.62ms | 14.33ms | 14.86ms | 15.71ms | 17.21ms |
| 2 | 600 / 601.1 | 200 | 0/0 | 9.34ms | 15.83ms | 18.75ms | 21.42ms | 27.00ms | 30.60ms |
| 3 | 1500 / 1498.7 | 200 | 0/0 | 21.07ms | 51.38ms | 90.68ms | 165.97ms | 165.97ms | 212.47ms |

Clear degradation 2→3 (p50 ×3, tail worse), consistent with [[perf-findings]]'s ~2,500 orders/s
matching ceiling.

### `latency-match`

| level | spam target/achieved | n | dead | timeout | min | p50 | p90 | p95 | p99 | max |
|---|---|---|---|---|---|---|---|---|---|---|
| 0 | 0 / 0.0 | 92 | 8 | 0 | 11.21ms | 18.37ms | 19.04ms | 19.36ms | 21.45ms | 21.45ms |
| 1 | 100 / 100.2 | 190 | 10 | 0 | 8.46ms | 11.69ms | 13.89ms | 14.70ms | 17.08ms | 19.06ms |
| 2 | 600 / 598.6 | 174 | 26 | 0 | 9.90ms | 16.43ms | 21.15ms | 23.60ms | 27.01ms | 30.54ms |
| 3 | 1500 / 1500.2 | 170 | 30 | 0 | 21.77ms | 46.20ms | 73.39ms | 90.44ms | 117.22ms | 124.38ms |

"dead" = self-supplied counterparty lost the ordering race (documented, not a bug — see cmd doc
comment). Rate trends up with load (8%→5%→13%→15%) but stays a minority; same degradation shape
as `latency-ack` at level 3.

**Spam's own match rate falls short of the intended 80%.** Prometheus (`me_core_orders_processed_total`)
during the level-3 window: 41.5% open, 30.1% cancelled, 28.4% filled, 0% partially_filled — implying
at least ~51% of IOC taker legs find nothing. Cause: ~17 concurrent spam workers cross the same
narrow price band through only 2 maker + 2 taker accounts, so a taker can lose the race to its own
still-in-flight maker or to another worker's taker. Left as-is: the ME is still under real
contention either way, and the measured account's own latencies are unaffected. A fix would mean
decoupling maker supply from taker demand (a standing liquidity pool instead of same-tick pairing)
— not done.

### `latency-cancel`

| level | spam target/achieved | span | n | dead/timeout | min | p50 | p90 | p95 | p99 | max |
|---|---|---|---|---|---|---|---|---|---|---|
| 0 | 0 / 0.0 | cancel_round_trip | 100 | 0/0 | 10.53ms | 11.54ms | 12.69ms | 13.05ms | 13.60ms | 13.82ms |
| 0 | 0 / 0.0 | cancel_full_lifecycle | 100 | 0/0 | 63.40ms | 65.27ms | 66.39ms | 66.77ms | 67.03ms | 69.06ms |
| 1 | 100 / 99.9 | cancel_round_trip | 200 | 0/0 | 8.28ms | 11.52ms | 15.10ms | 15.51ms | 16.52ms | 18.03ms |
| 1 | 100 / 99.9 | cancel_full_lifecycle | 200 | 0/0 | 61.94ms | 64.86ms | 68.26ms | 69.18ms | 70.37ms | 71.79ms |
| 2 | 600 / 600.3 | cancel_round_trip | 200 | 0/0 | 10.45ms | 17.75ms | 22.25ms | 25.79ms | 28.75ms | 32.81ms |
| 2 | 600 / 600.3 | cancel_full_lifecycle | 200 | 0/0 | 63.05ms | 71.22ms | 75.40ms | 79.52ms | 83.33ms | 86.87ms |
| 3 | 1500 / 1499.4 | cancel_round_trip | 198 | 0/0 | 24.73ms | 52.50ms | 89.60ms | 119.17ms | 178.71ms | 210.00ms |
| 3 | 1500 / 1499.4 | cancel_full_lifecycle | 198 | 0/2 | 80.33ms | 118.51ms | 199.29ms | 246.69ms | 372.24ms | 382.82ms |

Level 3 had 2 cancels that never succeeded after 5 retries (logged as "race losses," left resting
until teardown) — the 2 timeouts above. Same shape as the other two tests: the tail blows out hard
at level 3 (round_trip p99 178.71ms vs level 2's 28.75ms).

## Conclusions

- **Real saturation knee between level 2→3**, not gradual decay: p99 jumps 4-6x across all three
  tests, despite level 3 (~1,500 orders/s) being only ~60% of [[perf-findings]]'s ~2,500 orders/s
  tuned ceiling. Tail latency degrades well before nominal capacity — for any p99-sensitive SLA,
  target sustained load meaningfully below the ceiling, not at it.
- **Level 0 reading slower than level 1 in all three tests is a cold-start artifact, not signal.**
  Level 0 was, every time, the first invocation touching that flow (fresh accounts, empty DB
  pool, uncached query plans). Treat level-0 numbers as an upper bound on idle latency, not a
  clean baseline.
- **`cancel_full_lifecycle`'s tail (382.82ms max) is the worst number in the dataset.** Stacking
  two latency-sensitive legs (ack + cancel round trip) sequentially compounds tail risk — any
  "place then cancel" client workflow should expect worse tail latency under load than either leg
  alone suggests.
- **Spam's ~50% IOC-miss rate** (see above) means levels 2-3 represent "many small participants
  racing thin liquidity" more than "deep liquidity, high match volume" — a real load pattern, but
  not necessarily the one that matters most for capacity planning.
- **Confidence**: the qualitative shape (flat, then knee) is trustworthy — three independently
  coded measurement paths agree at every level. Exact p99/max values are noisy at n=170-200 (p99
  is ~2 data points) — read as order-of-magnitude, not precise.
- **Scope**: single laptop (see test machine above), single `api` + single `core` process, no
  network hops. This characterizes the matcher's own ceiling, not a production topology.
