# Observability — Metrics Reference

This documents every metric the system exposes today, what it answers, its type, and its labels.
It reflects what is **implemented and live**, not a plan. Custom metrics are emitted through
`common/pkg/observability` (`PrometheusMetrics` + `PrometheusServer`); each service also exposes Go
runtime/process metrics, and RabbitMQ exposes its own.

---

## Scrape targets

| Target | Endpoint | What it serves |
|--------|----------|----------------|
| **api** | `:9091/metrics` (`METRICS_PORT`) | `me_api_*`, `me_db_*` (`service="api"`), `go_*`, `process_*` |
| **core** | `:9092/metrics` (`METRICS_PORT`) | `me_core_*`, `me_db_*` (`service="core"`), `go_*`, `process_*` |
| **rabbitmq** | `:15692/metrics` | `rabbitmq_*` (queue depth / broker health) |

Prometheus scrapes all of the above (`local-deploy/prometheus/prometheus.yml`). Grafana dashboards:
**`me-api`**, **`me-core`**, **`me-db`** (`local-deploy/grafana/dashboards/`).

---

## Naming conventions

- Namespace **`me`** (matching engine), subsystem per module: **`api`**, **`db`**, **`core`**.
  Full name = `me_<subsystem>_<name>_<unit>`, e.g. `me_core_batch_duration_seconds`.
- Counters end in `_total`; histograms carry a unit suffix (`_seconds`); gauges are point-in-time.
- **Labels are bounded sets only** — never `order_id`, `user_id`, raw URL path, prices, or timestamps
  (see [Label cardinality](#label-cardinality)).

---

## `api` — subsystem `me_api_*`

Recorded by the HTTP `AccessLog` middleware and the order publisher (`api/internal/metrics`).

| Metric | Type | Labels | Answers |
|--------|------|--------|---------|
| `me_api_http_requests_total` | counter | `method, route, status` | Request volume and HTTP error rate |
| `me_api_http_request_duration_seconds` | histogram | `method, route, status` | Latency distribution (p50/p95/p99) per endpoint |
| `me_api_http_requests_in_flight` | gauge | — | In-flight requests right now (saturation) |
| `me_api_order_publish_total` | counter | `market, result` | Did the order reach RabbitMQ? `result` = `success` \| `error` |
| `me_api_order_publish_duration_seconds` | histogram | `market` | Latency of the API→broker publish hop |

`route` is the **matched route template** (`/api/v1/order/:id`), never the concrete path. Requests
that 404/405 are not counted (no bounded template).

---

## `db` — subsystem `me_db_*`

Emitted by **both** the `api` and `core` processes (the repository + `sql.DB` pool run embedded in
each), so every series carries a **`service`** label (`"api"` \| `"core"`). There is no standalone
`db` process. Pool stats come from a **scrape-time collector** reading `sql.DB.Stats()`
(`db/pkg/metrics`).

| Metric | Type | Labels | Answers |
|--------|------|--------|---------|
| `me_db_pool_connections_open` | gauge | `service` | Open connections (in use + idle) |
| `me_db_pool_connections_in_use` | gauge | `service` | Connections currently serving a query |
| `me_db_pool_connections_idle` | gauge | `service` | Idle headroom |
| `me_db_pool_wait_count_total` | counter | `service` | Times a caller blocked waiting for a connection (pool exhaustion) |
| `me_db_pool_wait_duration_seconds_total` | counter | `service` | Cumulative time spent blocked waiting for a connection |
| `me_db_query_duration_seconds` | histogram | `service, operation, result` | Per-operation query latency. `result` = `ok` \| `error` |
| `me_db_query_errors_total` | counter | `service, operation, class` | Query errors. `class` = SQLSTATE class (`22`, `23`, `40`, `08`, …) or `no_rows` / `context` / `other` |

**Instrumented `operation`s** (`OrderRepository` only): `get_order`, `get_orders_by_user`,
`get_orders_by_ids`, `load_open_orders`, `process_batch`. Other repositories (user/market/instrument)
are not yet instrumented.

> A domain-translated "not found" surfaces as `result=error, class=other` (the repository converts
> `sql.ErrNoRows` to a sentinel before the metric sees it). Filter `operation=get_order` for a clean
> error view, or treat `class=no_rows` as non-error where it does appear.

---

## `core` — subsystem `me_core_*`

The matching engine. One processor per market; per-order counters use pre-bound handles so the hot
path is allocation-free (`core/internal/metrics`). Metric values are derived from the committed
`BatchResult` and a read-only book snapshot — the order book itself stays pure.

| Metric | Type | Labels | Answers |
|--------|------|--------|---------|
| `me_core_orders_received_total` | counter | `market` | Throughput into the engine (orders accepted off the queue) |
| `me_core_orders_processed_total` | counter | `market, outcome` | Outcome mix (see values below) |
| `me_core_trades_total` | counter | `market` | Executed trades (fills) — match rate |
| `me_core_batch_size` | histogram | `market` | Orders per committed micro-batch (batching efficiency) |
| `me_core_batch_duration_seconds` | histogram | `market` | **Core latency SLI** — match + commit time per batch |
| `me_core_batches_total` | counter | `market, result` | Batch outcomes (see values below) |
| `me_core_reserve_rejections_total` | counter | `market` | Orders rejected at balance reservation (insufficient funds) |
| `me_core_poison_isolations_total` | counter | `market` | Batches that fell into per-order isolation |
| `me_core_dead_letters_total` | counter | `market` | Orders dead-lettered after the failure cap — **alert on any increase** |
| `me_core_book_rebuilds_total` | counter | `market` | Book hydrations triggered by a failed batch — **alert on rate** |
| `me_core_book_orders` | gauge | `market, side` | Resting order count per side (book depth) |
| `me_core_book_best_price` | gauge | `market, side` | Best bid / best ask (0 when that side is empty) |

- `outcome` ∈ `open` · `filled` · `partially_filled` · `cancelled` · `rejected`
- `result` ∈ `committed` · `transient_fail` · `poison_isolated`
- `side` ∈ `buy` · `sell`

`batch_*`, `reserve_rejections`, and the resilience counters live under `core` (the matcher owns the
batch loop) even though `ProcessBatch` physically runs in the repo — they are matching-engine SLIs.
The generic SQL view is `me_db_query_*`.

---

## Runtime & process (all services)

Each service registry preloads the standard collectors, so `/metrics` also exposes:

- `go_*` — goroutines, GC pauses, heap/alloc, threads (`collectors.NewGoCollector`).
- `process_*` — CPU seconds, resident/virtual memory, open FDs, start time (`collectors.NewProcessCollector`).

Useful for spotting goroutine/FD leaks or GC pressure under load.

---

## RabbitMQ (broker)

Scraped from the broker's own Prometheus plugin (`:15692`) — **not** re-instrumented in core. Key
series for this system:

- `rabbitmq_queue_messages_ready` — command backlog (consumer lag). The engine's "is core keeping
  up?" signal.
- `rabbitmq_queue_messages_unacked`, `rabbitmq_connections`, `rabbitmq_channels`, node memory/health.

> The broker is configured with `prometheus.return_per_object_metrics = false`, so
> `rabbitmq_queue_messages_ready` is an **aggregate across all queues** (no per-queue/per-market
> label). Set it to `true` for a per-market breakdown (costs one series per queue).

---

## Histogram buckets

| Histogram | Buckets (seconds, unless noted) |
|-----------|---------------------------------|
| `me_api_http_request_duration_seconds` | `0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5` |
| `me_api_order_publish_duration_seconds` | `0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1` |
| `me_db_query_duration_seconds` | `0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25` |
| `me_core_batch_duration_seconds` | `0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1` |
| `me_core_batch_size` | `1, 2, 4, 8, 16, 24, 32` (count, not seconds) |

> ⚠️ `me_core_batch_size` buckets top out at **32**, but `maxBatchSize` is now **128**. Batches larger
> than 32 all fall into the `+Inf` bucket, so `histogram_quantile` saturates at 32 and under-reports
> true batch size. Extend the buckets (e.g. `…, 32, 64, 96, 128`) to read it accurately.

---

## Label cardinality

| Label | Domain | Bound |
|-------|--------|-------|
| `service` | `api`, `core` | 2 |
| `market` | active markets | ~tens |
| `side` | `buy`, `sell` | 2 |
| `outcome` | open/filled/partially_filled/cancelled/rejected | 5 |
| `result` | success/error, or committed/transient_fail/poison_isolated | ≤3 |
| `operation` | logical query names | small fixed set |
| `class` | SQLSTATE class + sentinels | small fixed set |
| `method` | HTTP verbs | ~5 |
| `route` | route **templates** | bounded by router |
| `status` | HTTP status codes | small set |

**Never used as labels:** `order_id`, `user_id`, raw URL path, prices, timestamps, any unbounded id.

---

## Not yet emitted — event-log / outbox (ships with Queue 2)

The post-match event log (`orders.events`) is not built (`common/pkg/rabbitmq/exchange.go` is a
stub), so these metrics **do not exist yet**. They are pre-designed here so the instrumentation lands
with the queue. The intended implementation is a **transactional outbox** (events written inside the
match tx, drained by a publisher) — only that makes the consistency-lag SLI measurable; fire-and-forget
would lose events silently.

| Metric (planned) | Type | Labels | Answers |
|------------------|------|--------|---------|
| `me_core_outbox_backlog_rows` | gauge | `market` | Committed-but-unpublished events (publisher falling behind) |
| `me_core_outbox_oldest_unpublished_age_seconds` | gauge | `market` | **Consistency-lag SLI** — how stale the event stream is vs the ledger |
| `me_core_events_published_total` | counter | `market, event_type` | Publish throughput by type (`trade`, `order_update`, …) |
| `me_core_events_publish_errors_total` | counter | `market` | Publish failure rate |
| `me_core_events_publish_duration_seconds` | histogram | `market` | Publish latency to the exchange |
