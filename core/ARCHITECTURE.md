# Matching Engine — Messaging Architecture

## Overview

The system uses two RabbitMQ exchange/queue topologies with distinct responsibilities:

| | Queue 1 — Command | Queue 2 — Event log |
|---|---|---|
| Direction | API → core | core → world |
| Pattern | Work queue (competing consumers) | Pub/sub (topic exchange) |
| Producers | Many API instances | One core instance |
| Consumers | One core instance per market | WebSocket gateway, API read layer |
| Ordering required | Yes — strict FIFO per market | No |
| Durability | Persistent (durable queue + persistent messages) | Transient acceptable |

> **Implementation status.** Queue 1 and the matcher are implemented. **Queue 2 (the
> event log) is designed but not yet implemented** — `core` persists to Postgres but does
> not yet publish lifecycle/trade/orderbook events. For the implementation-level walk
> through (consumer, micro-batch transaction, balance settlement, and commit-failure
> recovery) see [`docs/`](docs/README.md). This file remains the messaging-topology
> reference.

---

## Queue 1 — Command queue (order submission)

### Exchange and binding topology

Use a **direct exchange** named `orders.commands`.

The API publishes with `routingKey = <marketID>`. The core declares one queue per active market and binds it to the exchange with the corresponding routing key:

```
orders.commands (direct exchange)
  ├── routingKey: btcusd  →  queue: orders.commands.btcusd  → core consumer A
  ├── routingKey: ethusd  →  queue: orders.commands.ethusd  → core consumer B
  └── routingKey: solusd  →  queue: orders.commands.solusd  → core consumer C
```

This gives you:
- **Strict FIFO ordering within a market** — one consumer per queue, no parallelism inside a market.
- **Parallelism across markets** — each market queue is processed independently.

### Order submission flow

```
Client → POST /orders
           │
           ▼
        API validates request
        Generates UUIDv7 order ID
           │
           ▼
        Publishes to orders.commands (routingKey=<marketID>)
           │
           ▼
        Returns 202 Accepted { order_id: "..." }
```

The API does not wait for the match result. The client tracks the final state via WebSocket (fed by Queue 2) or by polling `GET /orders/{id}`.

### Wire format

Every body on both queues is Protocol Buffers, `content-type: application/protobuf`. The schema
lives in `common/proto/me/v1/` (`OrderCommand` for this queue, `Event` for Queue 2), generated
into `common/pkg/pb/me/v1` (package `mev1`) by `make proto` (protoc + protoc-gen-go pinned to the
vendored runtime; the generated files are committed). No `.proto` names a Go import path: the
Makefile derives each file's package from its directory (`proto/me/v1` → `pkg/pb/me/v1`, named
after the directory without its slashes) and the module path from `common/go.mod`, so the
schema stays language-neutral and a new directory needs no configuration. Only the codecs touch
the generated types: `core/pkg/order_events_queue` and `common/pkg/marketdata` convert to and
from the domain structs the rest of the code uses.

Field 1 of every envelope is `uint32 version`, reserved in every schema version forever
(`common/proto/me/header.proto`, package `me`). A consumer reads it first (`me.Version`) and
decodes with the matching package; an incompatible change is a new `me.v2` directory next to
`me.v1`, never an edit to it. A body of a version this build does not know is refused as
`unsupported schema version N` — on this queue that dead-letters it as `malformed` with that
error text.

After a schema change: edit the `.proto`, `make proto`, commit the generated files, and run
`go work vendor` at the workspace root before any local build or Docker image (the root `vendor/`
is gitignored but, when present, forces `-mod=vendor`).

### Core consumer flow

```
core consumer (per market)
  ├── Receives order from queue
  ├── Validates order event (market constraints, field rules)
  ├── Pushes {event, ack, nack} to in-process channel  (does NOT ack)
  └── [matcher goroutine matches, persists, and acks after commit — see below]
```

The consumer is kept free of I/O — it validates (no DB access) and enqueues. Crucially it
does **not** acknowledge the message. Under **ack-after-commit**, ownership of the ack/nack
travels with the event to the matcher, which acknowledges only once the transaction that
persists the order has committed. A command that can never be processed — malformed, invalid,
or of an unknown type — is instead **rejected without requeue**: the broker moves it to the
market's dead-letter queue in that same operation (see *Dead letters* below), since it has no DB
effect and retrying it would only fail again.

> **Why not ack on enqueue?** Acking the moment the order is buffered would lose it on a
> commit failure — gone from the broker, never written to the DB. Deferring the ack to
> after commit makes "accepted and persisted" a single atomic fact. The cost is that
> in-flight (unacked) messages are redelivered after a crash; idempotent reprocessing
> (keyed on the `orders` table) makes that safe. See [`docs/order-lifecycle.md`](docs/order-lifecycle.md).

**Prefetch count:** `prefetchCount=16`. With ack-after-commit, prefetch bounds the number
of **unacked in-flight** messages to 16, which in turn caps the matcher's micro-batch and
provides natural backpressure if the matcher stalls (e.g. during a DB outage): once 16 are
unacked the broker stops delivering until the matcher commits and acks a batch. The
throughput win comes from amortising one `BEGIN/COMMIT` across the batch, not from hiding
per-message ack round-trips.

Ordering is preserved because the consumer loop (`for delivery := range deliveries`) is
sequential: messages are drained one at a time in FIFO arrival order, and the matcher
replays each batch in arrival order. A sequencer is only needed when messages are processed
concurrently across goroutines, which is not the case here.

---

### Matcher goroutine — micro-batch strategy

The matcher runs one goroutine per market and is the sole writer for that market. It reads from an in-process buffered channel (capacity 64) fed by the consumer.

**Why micro-batching:** Each order requires ~6 DB statements (INSERT order, UPDATE order, INSERT open_orders, INSERT cancelled_orders, UPDATE user_balances, INSERT matches). At one transaction per order, the `BEGIN/COMMIT` round-trip (~1–5 ms) dominates. Micro-batching amortises that cost across N orders.

```
Throughput comparison (approximate):
  1 tx / order  → ~100–200 orders/sec  (round-trip per order)
  10 orders/tx  → ~700–1000 orders/sec (round-trip per batch)
  20 orders/tx  → ~1000–2000 orders/sec
```

**Batch collection:**

```
for {
    batch = drain(ordersChannel, maxSize=32, maxWait=5ms)
    if channel closed and drained { break }     // matcher exits
    err = ProcessBatch(batch):                   // ONE transaction:
            phase 1  reserve funds per order     // atomic UPDATE; skip already-persisted
            phase 2  runMatchingOnBatch          // pure in-memory; funded orders only
            phase 3  flushBatchToDB              // bulk writes
          COMMIT
    if err == nil  → ack every message in the batch          // ack-after-commit
    else           → nack/requeue + rebuild book from DB     // recovery, see below
}
```

`drain` blocks on the first item (avoids busy-waiting), then collects additional items non-blocking until `maxSize` is reached or `maxWait` elapses. This keeps latency bounded: a lone order in a quiet market is committed within `maxWait` of arrival.

**Reservation gates matching.** Funds are blocked in **phase 1**, inside the same
transaction, *before* matching — so an order that cannot be funded never touches the book.
A failed reservation is a normal *rejection* (recorded as `cancelled`), not a transaction
error. This is what keeps the engine the single, atomic owner of orders **and** balances
(reserve → settle → release all commit together). The trade-off is a per-order conditional
`UPDATE` inside the batch; the heavy writes still go out in bulk.

**Ordering guarantee:** The matching algorithm runs on the batch sequentially in arrival order (FIFO from the channel), preserving price-time priority. The DB flush writes all results of the batch in a single transaction — atomicity is maintained across all orders in the batch.

**Recovery on commit failure.** Phase 2 mutates the in-memory book, but the writes are
durable only at `COMMIT`. If the transaction fails, Postgres rolls back while the book is
left dirty, so the matcher **nacks/requeues** the batch and **rebuilds the book from
`open_orders`** (`LoadOpenOrders` → `Hydrate`) before continuing. The requeued messages are
redelivered and reprocessed; idempotency (skipping orders already in the `orders` table)
makes the commit-ambiguous case safe. Full protocol in
[`docs/order-lifecycle.md`](docs/order-lifecycle.md#6-commit-failure-recovery).

**The transaction — reserve, match, then bulk flush:**

```sql
BEGIN;
  -- phase 1: per order, conditional reservation (skip if id already in `orders`)
  UPDATE user_balances SET balance = balance - $amt, blocked = blocked + $amt
    WHERE user_id = $u AND instrument_id = $i AND balance >= $amt;   -- 0 rows ⇒ reject

  -- phase 2: in-memory matching of funded orders (no SQL)

  -- phase 3: bulk flush of the BatchResult, FK-safe order
  INSERT INTO orders            VALUES (...),(...),...  ON CONFLICT (id) DO NOTHING;
  UPDATE orders SET status=...  FROM (VALUES ...) ...;     -- maker transitions
  INSERT INTO open_orders       VALUES (...),(...),...;    -- GTC remainder rests
  UPDATE open_orders SET ...    FROM (VALUES ...) ...;     -- partially-filled makers
  DELETE FROM open_orders       WHERE order_id = ANY($ids);-- filled / cancelled makers
  INSERT INTO cancelled_orders  VALUES (...),(...),...;
  INSERT INTO matches           VALUES (...),(...),...;    -- one row per fill
  UPDATE user_balances SET ...  FROM (VALUES ...) ...;     -- settlement + release
COMMIT;
```

> The bulk writes use **multi-row `VALUES`** rather than literal `UNNEST`: `lib/pq` cannot
> encode `NULL`s inside array parameters (nullable `client_order_id`, `have/want_quantity`,
> `expires_at`), and multi-row `VALUES` gives the same one-round-trip-per-table benefit while
> handling `NULL`s natively. Each statement is skipped when it has nothing to write.

---

## Dead letters

A command the matcher can never process is rejected without requeue. Every command queue is
declared with `x-dead-letter-exchange = me.dlx`, so the reject and the park are one broker
operation: the message lands on `me.dlq.<market>` (bound to `me.dlx` under the market ref) and is
never redelivered to the matcher, never dropped. Four things get rejected: a message that does
not parse (`malformed`), a well-formed order that fails `ValidateOrderEvent` against the market's
constraints (`invalid`), an envelope of a type core does not handle (`unknown_type`), and an
order that failed to commit deterministically `maxOrderFailures` times in isolation (`poison`).

Core runs one `deadletter.Consumer` per served market that drains that market's parking queue
into the `dead_letters` table, which is what operators read:

```
GET /admin/dlq?market=ETH-USDT&limit=50      → cli dlq list [--market] [--limit] [--json]
```

**The reason is recomputed, not remembered.** The broker records only that a message was
rejected (`x-death`: queue, count, time). Classification is a pure function of the payload and
the market's constraints — `deadletter.Classify`, the same function the matcher used on delivery —
so the consumer re-derives `malformed` / `unknown_type` / `invalid` exactly, at any time, after
any restart. A payload that classifies as processable can only be on the parking queue because it
was poison, so that is what it is recorded as. The single value that is not a function of the
payload is the poison error text (which DB constraint fired): the matcher hands it to the consumer
through an in-process `PoisonErrors` map just before rejecting, best effort; a restart between the
reject and the consume leaves `"see core log at dead_at"` on that one row. Two consequences: the
consumer must run in the same process as the matcher that rejects (hence one parking queue per
market, drained by the core serving it), and it validates against the market's *current*
constraints, so a market edited in that same window could re-classify an `invalid` as `poison`.

The consumer acks only after the row is written; a database error nacks the message and waits a
second before the next attempt (`me_core_dlq_persist_failures_total`), so a dead letter can be
delayed but not lost. The insert skips a row that already exists for the same message, market,
event type and dead-at, so a redelivery after a crash between insert and ack adds nothing.
`dead_letters.payload` is JSONB: the command rendered with `protojson` when the body decodes
(`"version"`, then `"open"` or `"cancel"`; UUIDs show as base64 because they are `bytes` on the
wire), otherwise `{"raw_base64": "…"}` holding the exact bytes, so a malformed or wrong-version
body is kept whole. `dead_letters.status` is `parked` for every row today; replay / discard will
be an `UPDATE` on it plus a republish of the payload re-encoded from either shape.

⚠️ Upgrading to this topology is breaking: a queue cannot gain `x-dead-letter-exchange` after
the fact (`PRECONDITION_FAILED`, and both api and core crash-loop on the declare). Delete the
command queues before starting the new binaries — `make stack-down` (the deps compose drops its
volumes) or `rabbitmqctl delete_queue <market>` per market — and run migration `000005`.

## Queue 2 — Event log (core → world)

> **Status: designed, not yet implemented.** The matcher persists to Postgres but does not
> yet publish to an event log; the `emit*` hooks in the order book currently produce only
> database side-effects. The topology below is the target design. The `seq`, snapshot, and
> delta mechanics are likewise not built.

### Exchange and binding topology

Use a **topic exchange** named `orders.events`.

Core publishes with typed routing keys. Each downstream consumer declares its own queue and binds with a pattern that matches only what it needs:

```
orders.events (topic exchange)
  │
  ├── order.created.<marketID>
  ├── order.filled.<marketID>
  ├── order.partially_filled.<marketID>
  ├── order.canceled.<marketID>
  ├── trade.executed.<marketID>        ← a match between two resting orders
  ├── orderbook.snapshot.<marketID>    ← full state, on core startup + periodic
  └── orderbook.delta.<marketID>       ← incremental update after each order processed
```

Consumer binding examples:

| Consumer | Binding pattern | Gets |
|---|---|---|
| WebSocket gateway | `order.*.*` | All order lifecycle events |
| Market data feed | `trade.#` | All trade executions |
| Orderbook reader | `orderbook.#` | Snapshots and deltas |
| Audit log | `#` | Everything |

Adding a new consumer requires no changes to the producer.

### Routing key catalog

| Routing key | Payload | When |
|---|---|---|
| `order.created.<marketID>` | `{ order_id, user_id, side, type, price, qty, timestamp }` | Order accepted and queued |
| `order.filled.<marketID>` | `{ order_id, fill_price, fill_qty, trade_id }` | Order fully matched |
| `order.partially_filled.<marketID>` | `{ order_id, fill_price, fill_qty, remaining_qty, trade_id }` | Partial match |
| `order.canceled.<marketID>` | `{ order_id, reason }` | Canceled by user or TTL |
| `trade.executed.<marketID>` | `{ trade_id, maker_order_id, taker_order_id, price, qty, timestamp }` | A match occurred |
| `orderbook.snapshot.<marketID>` | See below | Core startup + periodic |
| `orderbook.delta.<marketID>` | See below | After every order processed |

---

## Orderbook exposure via API — Option A (in-memory, event-driven)

### Design principle

The API never queries the DB for orderbook state. Instead, each API instance subscribes to the `orderbook.*` events from Queue 2 and maintains its own in-memory snapshot. HTTP requests for the orderbook are served directly from memory.

```
core
  └── publishes orderbook.snapshot + orderbook.delta to orders.events
           │
           ▼
api instance 1  ──── in-memory orderbook ────► GET /markets/{id}/orderbook
api instance 2  ──── in-memory orderbook ────► WebSocket stream
```

### Event structures

**Snapshot** (`orderbook.snapshot.<marketID>`):

```json
{
  "market_id": "btcusd",
  "seq": 10042,
  "bids": [
    { "price": "65000.00", "qty": "1.500" },
    { "price": "64990.00", "qty": "0.800" }
  ],
  "asks": [
    { "price": "65010.00", "qty": "2.000" },
    { "price": "65020.00", "qty": "1.200" }
  ]
}
```

**Delta** (`orderbook.delta.<marketID>`):

```json
{
  "market_id": "btcusd",
  "seq": 10043,
  "changes": [
    { "side": "bid", "price": "65000.00", "qty": "0.500" },
    { "side": "ask", "price": "65010.00", "qty": "0.000" }
  ]
}
```

A `qty` of `"0.000"` means that price level is now empty and should be removed from the book. Core always emits all levels touched by the order — filled, partially filled, or newly placed.

The `seq` field is a monotonically increasing integer, incremented by core for every event it publishes for a given market.

### Bootstrap sequence on API startup

A new API instance cannot apply deltas without first knowing the current state of the book. The bootstrap process:

```
1. API subscribes to orderbook.# for the target market.
2. API starts buffering all incoming delta events in a temporary queue (in memory).
3. API sends a snapshot request to core via a dedicated request queue
   (or simply waits — see below).
4. Core publishes a fresh orderbook.snapshot.
5. API receives the snapshot, records its seq number N.
6. API applies all buffered deltas with seq > N in order, discarding those with seq ≤ N.
7. API enters normal operation: applies each incoming delta immediately.
```

**Simpler alternative for PoC:** Core publishes `orderbook.snapshot.<marketID>` on startup and every 30 seconds thereafter. New API instances wait for the next periodic snapshot (at most 30 s) before serving orderbook requests. During the wait, return `503 Service Unavailable` on orderbook endpoints.

### Gap detection

Each delta carries a `seq`. The API tracks the last applied `seq` per market. If an incoming delta has `seq != last_seq + 1`, there is a gap (missed message). On gap detection:

1. Drop all buffered state for that market.
2. Mark the orderbook as stale.
3. Wait for the next snapshot to re-bootstrap.
4. Return `503` on orderbook requests until re-bootstrapped.

Gaps are rare (they require a consumer outage or broker issue) but must be handled to avoid serving a corrupted book.

### WebSocket delivery

The API's WebSocket handler subscribes to an internal Go channel that is updated by the orderbook consumer goroutine:

```
orderbook consumer goroutine
  ├── receives delta from RabbitMQ
  ├── applies delta to in-memory map[marketID]*Orderbook
  └── broadcasts updated levels to all active WebSocket subscribers for that market
           │
           ▼
        subscriber 1 (client A watching btcusd)
        subscriber 2 (client B watching btcusd)
```

Broadcast is done via a Go channel per WebSocket connection with a small buffer (e.g. 64). If the buffer is full (slow client), drop the update for that client only — never block the broadcast loop. The client will receive the next delta and can detect a gap via its own `seq` tracking.

### Consistency across multiple API instances

Because all API instances subscribe to the same topic exchange and each gets its own dedicated queue, every instance receives every event independently. As long as all instances bootstrap from the same snapshot and apply deltas in the same order (guaranteed by RabbitMQ's per-queue FIFO), they will all converge to identical in-memory state.

There is no shared cache, no inter-instance coordination, and no single point of failure on the read path.

### Trade-offs of this approach

| Pro | Con |
|---|---|
| No Redis dependency | Each API instance uses memory proportional to book depth × markets |
| Sub-millisecond read latency | New instances have a cold-start delay (up to snapshot interval) |
| Scales horizontally without coordination | A crash loses in-memory state (must re-bootstrap) |
| Orderbook always consistent with event stream | Snapshot interval = max staleness window for new instances |

---

## Summary

```
                    ┌─────────────────────────────────────┐
                    │              RabbitMQ                │
                    │                                      │
  API instances ───►│  orders.commands (direct exchange)  │───► core (per-market consumer)
                    │                                      │         │
                    │  orders.events (topic exchange)     │◄────────┘
                    │    order.*                           │
                    │    trade.*                           │───► WebSocket gateway
                    │    orderbook.*                       │───► API in-memory orderbook
                    └─────────────────────────────────────┘
                                                                      │
                                                              GET /orderbook  (served from memory)
                                                              WebSocket push  (broadcast on delta)
```
