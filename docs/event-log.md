# Event-Log — Live Market-Data Fan-Out

A second RabbitMQ pathway (`common/pkg/rabbitmq/exchange.go`) that streams matching-engine
events from `core` to every `api` instance, which relay them to clients (bots, UIs) over
Server-Sent Events. No database involvement.

---

## 1. Purpose — and what it is *not*

The engine has a durable record of truth already (the Postgres ledger + the durable command
queue). This pathway does **one** job: **disseminate live events** so clients can render trades
and the order book in real time, and bots can react to fills.

It is **ephemeral and best-effort by design**: it persists nothing. If a consumer misses events
(was down, restarting), it re-synchronises from the next periodic snapshot — it does not replay
history.

| | Durable outbox (separate, future) | Live event fan-out (this doc) |
|---|---|---|
| Goal | No consumer ever misses an event | Push live updates to UIs/bots now |
| Loss on crash | Unacceptable | Fine — client re-snapshots |
| Storage | Persisted table | None |

---

## 2. Topology

The command queue is a **work queue**: competing consumers, each order processed exactly once.
This pathway is a **fan-out**: every API instance receives every event it is subscribed to.

| | Command queue (Queue 1) | Event exchange (this) |
|---|---|---|
| RabbitMQ primitive | durable shared queue | **topic exchange** `me.events` + one **exclusive, auto-delete, non-durable** queue **per API instance** |
| Delivery | load-balanced (msg → one consumer) | **fan-out** (msg → every instance) |
| Durability / ack | durable, ack-after-commit | transient, auto-ack |
| Publisher | api | **core** (single writer per market) |

A **topic** exchange (not fanout) so the routing key carries `market` + event type, and an
instance binds only to what it needs:

```
routing key:  market.<market>.<type>     e.g. market.BTC-USDT.trade
              user.<user_id>.<type>       e.g. user.<uuid>.order
bindings:     market.BTC-USDT.#           (all public events for one market — bound at startup)
              user.<uuid>.#               (one user's private stream — bound while connected)
```

These helpers live in `common/pkg/marketdata` (`PublicKey`, `PrivateKey`, `MarketBinding`,
`UserBinding`, `TypeBinding`).

---

## 3. Events

Events are split into **public** (anyone) and **private** (only the owning user). This split is
a **security boundary** enforced at the broker, not a convenience — see ⚠️ below.

Wire format: every event is a JSON `Envelope` (`common/pkg/marketdata`):

```go
type Envelope struct {
    Epoch   string          // per-core-instance UUID — changes on every core restart
    Seq     uint64          // per-market monotonic counter, advanced on each book delta
    Type    EventType       // "trade" | "book" | "heartbeat" | "snapshot" | "order"
    Market  string          // omitted for private events
    Ts      int64           // unix milliseconds
    Payload json.RawMessage // type-specific DTO below
}
```

Amounts in the internal `core→api` envelope are `uint64` (native ticks). JS-safe string
encoding is applied at the SSE edge, not here.

### Public — routing key `market.<m>.<type>`

| Type | Payload | Purpose |
|------|---------|---------|
| `trade` | `Trade{price, quantity, taker_side}` | The trade tape / last price. **No identities.** |
| `book` | `Book{side, price, quantity}` (`quantity=0` ⇒ level removed) | Net L2 delta at native price resolution |
| `heartbeat` | `Heartbeat{}` (epoch/seq in envelope) | Keeps SSE connections warm and lets an idle consumer detect a sequence gap |
| `snapshot` | `Snapshot{epoch, seq, market, bids, asks}` | Full L2 book at a sequence point (periodic broadcast) |

### Private — routing key `user.<user_id>.<type>`

| Type | Payload | Purpose |
|------|---------|---------|
| `order` | `OrderUpdate{order_id, status, filled, remaining}` | open / partially_filled / filled / cancelled / rejected |

> ⚠️ **Public vs private is a security boundary.** The public book is published as **L2
> (price-level aggregates, no identity)**. Per-user order lifecycle is routed to per-user keys
> (`user.<uid>.order`) and isolated by the broker: an API instance only receives a user's
> private events while that user is connected to it, and the payload deliberately carries no
> `user_id` — the routing key is the identity.

---

## 4. Sequence numbers — crash detection and re-sync

Every public event carries `(Epoch, Seq)` per market. `Seq` is a per-market monotonic counter
advanced once per `book` delta. Snapshots, heartbeats, trades, and order updates are stamped
with the **current** seq, so a book consumer's `seq+1` check never sees a false gap.

`Epoch` is a fresh UUID generated when `core` starts (shared across all markets on that
instance). An epoch change means core restarted and the book was rebuilt from scratch.

### API cache sync (per market)

1. Bind `market.<m>.#`, start consuming. **Ignore deltas until the first `snapshot` arrives.**
2. Load `snapshot(S)` → `cache = book@S`, `lastSeq = S`.
3. For each following `book` delta: if `seq == lastSeq+1`, apply it and advance `lastSeq`.
4. **Gap** (`seq` skips) or **epoch change**: discard cache, wait for the next snapshot,
   re-sync. Self-healing.

Because RabbitMQ preserves per-routing-key order from the single core publisher, anything
published after `snapshot(S)` is guaranteed `seq > S` — no buffering or dedupe race. `seq`
exists only for loss/restart detection, not reordering.

### Snapshot transport — periodic broadcast

Core publishes a full per-market snapshot to the exchange on a ticker, alongside the continuous
deltas. No RPC: the in-order stream makes consumer sync trivial. New API instances wait at most
one tick interval before their first snapshot arrives; during that window they serve their
currently unsynced (empty) cache.

---

## 5. Price granularity — native L2 on the wire, bucketing at the API edge

`price_quantum` is the tick and therefore the finest possible resolution: every price is a
multiple of it, so aggregation is one-way and lossy. Core publishes at native resolution;
coarser views are computed at the edge.

**Core** publishes one `Book{side, price, quantity}` delta per **occupied price level** (not
per tick; the book only holds levels where orders rest).

**The API** buckets per subscription. A client requests grouping `G = k × price_quantum` (`k ≥ 1`,
validated against the market's `price_quantum`):
- **bids floor, asks ceil** to their bucket — so the displayed spread is never tighter than reality.
- On each delta, the Hub updates the canonical L2 and recomputes only the **affected bucket**;
  one aggregated view is maintained **per distinct grouping in use** (not per client).

Trades stream individually — they are unaffected by bucketing.

Example (bids, `price_quantum=1`, `G=5`): canonical `103→4, 102→1, 101→2, 98→10` ⇒ client
at `G=5` sees `100→7, 95→10`; client at `G=1` sees all four. One canonical cache, many views.

---

## 6. End-to-end flow

Two API instances, two users, showing who receives what and why.

```
                     ┌──────────────────────── core (1 matcher / market) ───────┐
                     │  holds live book · assigns (epoch,seq) · publishes events │
                     └───────────────┬───────────────────────────────────────────┘
                                     │ publish (off hot path — via publisher goroutine)
                                     ▼
                        ┌──────────  me.events  (topic exchange)  ──────────┐
                        │  routes each msg by routing key to bound queues   │
                        └───────┬───────────────────────────────┬───────────┘
          market.BTC-USDT.#  ◄──┤                               ├──►  market.BTC-USDT.#
                user.A.#     ◄──┤                               ├──►  user.B.#
                     ▼                                               ▼
              ┌──── API-1 ────┐                              ┌──── API-2 ────┐
              │ L2 book cache │                              │ L2 book cache │
              │ SSE: Alice(A) │                              │ SSE: Bob(B)   │
              └──────┬────────┘                              └──────┬────────┘
                     ▼                                              ▼
               Alice's bot (SSE)                              Bob's UI (SSE)
```

**Standing state.** Each API instance has **one** subscriber queue. It always binds the public
key for the markets it serves (`market.BTC-USDT.#`) at startup. It binds a private key
`user.<uid>.#` **only while that user is connected to it**.

**Binding lifecycle.**
1. API starts → binds `market.BTC-USDT.#`; syncs book cache. No private bindings yet.
2. Alice opens an authenticated SSE connection to API-1 → API-1 authenticates her as user `A`
   → **adds binding `user.A.#`** (ref-counted by connection count) and registers her connection.
3. Alice disconnects → drop the connection; if no Alice connections remain, **remove `user.A.#`**.

So API-1 is bound to `user.A.#` (not `user.B.#`); API-2 to `user.B.#` (not `user.A.#`).

**An order matches.** Bob's limit sell crosses Alice's resting buy. Core commits the batch and
derives **four** events from that one `BatchResult`:

| Event | Routing key | For |
|---|---|---|
| `trade` | `market.BTC-USDT.trade` | everyone |
| `book` (Alice's level shrank) | `market.BTC-USDT.book` | everyone |
| `order` (Alice filled) | `user.A.order` | Alice only |
| `order` (Bob's update) | `user.B.order` | Bob only |

**The broker routes by binding:**
- `market.BTC-USDT.trade` / `.book` → match `market.BTC-USDT.#` → delivered to **both** instances.
- `user.A.order` → matches only API-1's `user.A.#` → **API-1 only**. API-2 never receives it.
- `user.B.order` → matches only API-2's `user.B.#` → **API-2 only**.

**API → SSE fan-out (last hop).** API-1 applies the `book` delta to its cache, forwards
`trade` + bucketed `book` delta to every BTC-USDT SSE client, and forwards `user.A.order` to
Alice's connection only (matched by the authenticated uid). API-2 does the same for Bob. Each
user sees public market data plus their own fills; neither sees the other's private events —
enforced by broker routing **and** the per-connection uid index lookup.

**Initial state by stream type:**
- **Public book** — the API serves its current cached book (bucketed to the client's grouping)
  as the first SSE frame, then live deltas. `(epoch, seq)` is internal to the `core↔api` hop;
  clients never see it.
- **Private orders** — the client calls the existing REST `GET /orders` once at connect for
  current open orders, then applies live `order` events over SSE last-writer-wins per
  `order_id`. No private snapshot or private book cache in the API.

---

## 7. SSE endpoints

| Endpoint | Auth | Stream |
|----------|------|--------|
| `GET /api/v1/stream/:market` | none (market data is public) | `book`, `trade`, `heartbeat` frames; first frame is a full snapshot |
| `GET /api/v1/stream/users` | required | `order` frames for the authenticated user's orders across all markets |

The static `users` route is registered **before** `:market` so it is not captured as a market name.

**Slow consumer handling.** Each SSE client has a small per-connection buffer. If the buffer is
full the client is dropped (channel closed → reconnect and re-snapshot) so one slow client
never stalls the Hub.

---

## 8. Concurrency — the book stays lock-free

The order book is single-writer by design (driven solely by the matcher goroutine); a mutex
would put lock contention on the hottest path. The book stays lock-free by never letting a
second goroutine touch it:

- **Deltas** (`trade`, `book`) are derived from the `BatchResult` the matcher just committed —
  the matcher's own data, not the live book (`orderbook.DrainStream`).
- **Snapshots** are read from the book on the **matcher goroutine itself** via `SnapshotLevels`,
  then serialized.
- Both are serialized to `[]byte` and handed to a separate **publisher goroutine** via a
  buffered channel (capacity 4096 in `core/pkg/marketevents`). That goroutine only moves bytes
  — it never reads the book.
- **Snapshot and heartbeat timers live in the matcher's `select` loop**, so they fire even when
  idle without another goroutine reaching into the book.

```
matcher goroutine (owns book, no lock):
  select {
    case batch := drain():    process → DrainStream() → bytes → ch
    case <-snapshotTick:       SnapshotLevels() → bytes → ch
    case <-heartbeatTick:      Heartbeat → bytes → ch
  }
publisher goroutine:  for o := range ch { exchange.Publish(o) }   // bytes only; drop-on-full
```

The API Hub uses the same pattern: a single goroutine owns every market's `bookCache` and the
SSE client registry (`api/internal/stream`). All inputs — parsed events, register, unregister —
are funnelled through one `select` loop, so neither the cache nor the registry needs a lock.
The snapshot/delta join race is gone: a client is registered (and sent its initial snapshot)
atomically with respect to delta delivery.

---

## 9. Back-pressure and drop policy

Publishing is **never allowed to block the matcher**.

- `Enqueue` (matcher → publisher goroutine) is a non-blocking channel send. If the 4096-slot
  buffer is full the event is counted (`me_core_stream_events_dropped_total`) and dropped.
- A failed broker publish after the exchange's own reopen-retry is also counted and dropped
  (`me_core_stream_publish_errors_total`).
- Consumers detect a drop via a `seq` gap on the next event and wait for the next periodic
  snapshot to re-sync. No explicit notification is needed.

Alert thresholds: any sustained rate of `stream_events_dropped_total` or
`stream_publish_errors_total` indicates the publisher is falling behind the broker.

---

## 10. Key packages

| Package | Role |
|---------|------|
| `common/pkg/marketdata` | Wire contract: `Envelope`, event types, payload DTOs, routing-key helpers, `ExchangeName` |
| `common/pkg/rabbitmq` (`Exchange`, `Subscriber`) | Transport: durable topic exchange publisher; per-instance transient subscriber with `BindPattern`/`UnbindPattern` |
| `core/pkg/marketevents` | Core-side async publisher: buffered channel (4096), drop-on-full, single goroutine |
| `core/internal/orderbook` (`stream.go`) | Event derivation: `DrainStream` (net book deltas + order updates per batch), `SnapshotLevels`, `RecordRejection` |
| `core/internal/orderprocessors` | Per-market `(epoch, seq)` assignment; snapshot + heartbeat tickers in the matcher `select` loop |
| `api/internal/stream` (`hub.go`, `cache.go`, `bucket.go`) | Hub actor, `bookCache` sync state machine, `groupView` bucketing |
| `api/internal/stream` (`handler.go`, `router.go`, `client.go`) | SSE endpoints, market + user client types |
