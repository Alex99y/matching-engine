---
name: generate-openapi-from-go
description: Generates api/internal/openapi/swagger.yaml (OpenAPI 3.0.3) from the Go API's router.go and handler.go files under api/internal/*/. Reads every resource's routes, request/response structs, validate: tags, and error branches, then writes a single OpenAPI document. Call this skill manually after adding, removing, or changing an endpoint in api/internal/<resource>/{handler,router}.go or api/internal/server/server.go — the same trigger point as the sync-sdk-from-go skill. Does not touch the TypeScript SDK; run sync-sdk-from-go separately for that.
---

# Generate OpenAPI spec from the Go API

Produces `api/internal/openapi/swagger.yaml`, the source file the API server
`go:embed`s and serves at `GET /openapi.yaml`. Run this manually after an
endpoint changes — nothing regenerates this automatically, and the running
server won't pick up a new spec until the API is rebuilt and redeployed. See
the note at the end about why this is a manual, best-effort document rather
than a drift-proof one.

> ⚠️ This is an LLM read of the Go source, not an AST/reflection-based
> generator. It captures the same nuance `sync-sdk-from-go` does (validate
> tags, business-rule status codes) but two runs are not guaranteed to
> produce byte-identical YAML, and nothing fails a build if someone changes a
> handler and forgets to rerun this. Treat the output as a strong draft; spot
> check anything security-sensitive (auth requirements, scopes) by hand.

## Source-of-truth layers

| Go source (truth) | What to extract |
|---|---|
| `api/internal/<r>/router.go` | HTTP method + path per route, the resource's group prefix, and which middleware wraps each route (`auth`, `middleware.RequireWrite`, `middleware.RequireLoginOrigin`, `validations.ValidateContentType`) |
| `api/internal/server/server.go` | The parent group prefix (`/api/v1`) each resource is mounted under, plus any routes registered directly on `app` (e.g. `/health`) |
| `api/internal/<r>/handler.go` | Request structs (`c.Bind().Body(&req)`), their `json:`/`validate:` tags, response structs returned via `c.JSON`/`c.Status(...).JSON`, query/path params read via `c.Query`/`c.Params`, and the HTTP status per `errors.Is` branch |
| `api/pkg/utils` (`ErrorResponse` type, `NewErrorResponse`/`NewServerErrorResponse`) | The shared error-body shape (`{"message": string}`) reused by every 4xx/5xx response |
| `api/pkg/middleware/auth.go` | The bearer-token scheme, and what `RequireWrite`/`RequireLoginOrigin` mean, for the security-scheme description |

---

## Step 1 — Discover resources

```bash
ls api/internal/*/router.go
```

Every directory with a `router.go` is a resource to document (currently:
`candles`, `instruments`, `markets`, `orders`, `sessions`, `stream`, `users`).
Do not hardcode this list — a new resource directory with a `router.go` must
be picked up automatically next run. Directories without a `router.go`
(`metrics`, `config`, `server`, `openapi`) are not routed resources — skip
them.

Read, in full, for each resource: `router.go` and `handler.go`. Then read
`api/internal/server/server.go` once for the mount prefixes.

---

## Step 2 — Derive the full path per route

Same rule as `sync-sdk-from-go`: never read a path off the handler alone.

1. `server.go`: find `apiV1 := app.Group("/api/v1")` and the
   `<resource>.Register<Resource>Routes(apiV1, ...)` call — that's the parent
   prefix. Routes registered directly on `app` (like `/health`) have no
   parent prefix.
2. `router.go`: find the resource's own group (`app.Group("/order")`) and
   each route's method + sub-path (`.Get("/:id", ...)`).
3. Full path = parent prefix + resource group + sub-path, e.g.
   `/api/v1` + `/order` + `/:id` → `/api/v1/order/:id`. Fiber path params
   (`:id`, `:market`, `:symbol`) become OpenAPI `{id}`, `{market}`,
   `{symbol}` path parameters, each `required: true`, typed from how the
   handler parses them (`uuid.Parse` → `format: uuid`; otherwise `string`).

---

## Step 3 — Derive security per route

Read the middleware chain in `router.go` for each route, in order:

| Middleware present | OpenAPI effect |
|---|---|
| none (no `auth` in the chain) | no `security` on the operation (public) |
| `auth` (i.e. `fiber.Handler(authMiddleware)`) | `security: [{ bearerAuth: [] }]` |
| `auth` + `middleware.RequireWrite` | `security: [{ bearerAuth: [] }]`, note in the description: "requires a write-scoped session" |
| `auth` + `middleware.RequireLoginOrigin` | `security: [{ bearerAuth: [] }]`, note in the description: "requires a login-origin session (a minted token cannot call this)" |
| `validations.ValidateContentType(validations.ContentTypeJSON)` | request body `content` is `application/json` only, and document a 400 `Content-Type must be application/json` response |

Declare one shared `bearerAuth` security scheme (`type: http, scheme: bearer`)
in `components.securitySchemes` — every resource reuses it, there is exactly
one auth mechanism in this API.

---

## Step 4 — Extract request/response schemas

From each handler function, in order:

- **Request body**: the struct passed to `c.Bind().Body(&req)`. Map each
  field's `json:` tag to a schema property; a field is `required` in the
  schema only if its `validate:` tag contains `required` (an `omitempty` or
  absent-validate field is optional). Carry `validate` constraints through
  where OpenAPI has a direct equivalent: `min=N`/`max=N` on a string →
  `minLength`/`maxLength`; `oneof=a b` → `enum: [a, b]`. A request that binds
  `[]T` (batch endpoints like order create/cancel) is an array-of-object body
  — note the `MaxBatchSize` cap (see the resource's constant) as a comment
  and as `maxItems` if the array is capped.
- **Query params**: every `c.Query("name")` call in the handler becomes an
  OpenAPI query parameter. Infer `required` from whether the handler 400s on
  empty/missing (e.g. candles' `from`/`to` are required; orders' `limit` is
  optional with a default).
- **Response body**: the struct(s) passed to `c.JSON(...)` /
  `c.Status(...).JSON(...)`, one schema per distinct response struct, named
  after the Go type (`OrderResponse`, `BatchCreateOrderResponse`, ...).
  Pointer fields (`*T`) are optional / `nullable` in the schema, mirroring
  `omitempty` in the `json:` tag.
- **Error responses**: for every `errors.Is(err, Err...)` branch, add the
  response status code the branch maps to, with the shared `ErrorResponse`
  schema (`{"message": string}`, from `api/pkg/utils`). Every operation also
  gets a `500` with `ErrorResponse` (`NewServerErrorResponse` is the
  catch-all) — add it once as `components.responses.InternalError` and
  `$ref` it everywhere instead of repeating the schema.

---

## Step 5 — Go → OpenAPI type mapping

| Go type | OpenAPI type | Notes |
|---|---|---|
| `string` | `type: string` | |
| `int`, `int32` | `type: integer, format: int32` | |
| `int64` (amount: price/quantity/qty/quantum/size/remaining/balance/blocked) | `type: integer, format: int64` | still a plain JSON number on the wire — this API does not use string-encoded bigints |
| `int64` (`*_at` timestamp) | `type: integer, format: int64`, description: "unix seconds" | |
| `uint64` | `type: integer, format: int64`, `minimum: 0` | OpenAPI/JSON Schema has no native uint64; document the floor explicitly |
| `bool` | `type: boolean` | |
| `*T` | same as `T`, mark absent from `required` | do not add `nullable: true` unless the handler can explicitly emit JSON `null` |
| `[]T` | `type: array, items: <T schema>` | |
| `uuid.UUID` | `type: string, format: uuid` | |
| `time.Time` | `type: string, format: date-time` | |
| custom string-const type (`order_events_queue.OrderSide` etc.) | `type: string, enum: [...]` | list every const value from the source package |

---

## Step 6 — Assemble the document

Target **OpenAPI 3.0.3** (not 3.1) — broader Swagger UI / Redoc / codegen
compatibility than 3.1's stricter JSON Schema alignment, and nothing in this
API's types needs 3.1-only features.

```yaml
openapi: 3.0.3
info:
  title: Matching Engine API
  version: <bump only if asked; otherwise keep the previous value>
  description: >-
    <one paragraph, mention bearer-token auth and read/write session scopes>
servers:
  - url: /api/v1
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
  schemas:
    ErrorResponse: { ... }
    <one entry per request/response struct> ...
  responses:
    InternalError:
      description: Internal server error
      content:
        application/json:
          schema: { $ref: '#/components/schemas/ErrorResponse' }
paths:
  /order:
    post: { ... }
    get: { ... }
  /order/{id}:
    get: { ... }
  ...
```

Group `paths` in the same order the resources are mounted in `server.go`
(sessions, users, instruments, markets, orders, candles, stream), and within
a resource in the order routes appear in `router.go`. This keeps re-runs'
diffs small and reviewable.

**SSE endpoints** (`stream.MarketStream`, `UserStream`, `CandleStream`): these
never return JSON — document the response as
`content: { text/event-stream: { schema: { type: string } } }` with a
`description` explaining it's a long-lived Server-Sent-Events connection, and
point to `docs/event-log.md` for the frame format rather than trying to
model every event type as a schema.

**`/health`**: registered directly on `app` in `server.go`, no `/api/v1`
prefix, no auth, no request/response body worth modeling beyond `200 OK` —
document it minimally.

---

## Step 7 — Write and validate

Write the assembled document to `api/internal/openapi/swagger.yaml`
(overwrite in place — it's a generated file, diff it in the PR like any
other generated artifact).

Validate it parses as valid YAML before finishing:

```bash
python3 -c "import yaml,sys; yaml.safe_load(open('api/internal/openapi/swagger.yaml'))" && echo OK
```

If `npx` is available and network access is allowed, additionally lint the
document's OpenAPI structure (not just YAML syntax):

```bash
npx --yes @redocly/cli lint api/internal/openapi/swagger.yaml
```

Skip the `redocly` pass silently if `npx` isn't available or the sandbox has
no network access — the plain YAML parse check is the only hard requirement.

---

## Step 8 — Report

```
## OpenAPI spec generated

Source: api/internal/{candles,instruments,markets,orders,sessions,stream,users}/{router,handler}.go
Output: api/internal/openapi/swagger.yaml

Resources documented: <n> (<list>)
Paths: <n>
New/changed since last generation: <list, or "first generation">
YAML syntax: valid | <error>
Redocly lint: pass | skipped (no npx) | <errors>

Reminder: the running API serves this file via go:embed — rebuild and
redeploy the api module for this update to reach GET /openapi.yaml.
```

---

## Edge cases

- **Batch endpoints** (`CreateOrder`, `CancelOrder`): body is an array; each
  element's own validation failure surfaces per-item in the response array
  (`BatchCreateOrderResult.error`), not as a 4xx for the whole request —
  document the batch's own array-level errors (empty array, size cap) as
  400/422, and the response schema's per-item `error` field as the place
  per-order failures show up.
- **Multiple response shapes for one route**: `CreateOrder` returns 202 (all
  queued), 207 (partial), or 422 (all rejected) with the *same* response
  schema (`BatchCreateOrderResponse`) — document one schema shared across
  multiple `responses` status codes rather than duplicating it.
- **Struct embedding**: flatten embedded fields into the schema, same as the
  SDK skill.
- **No matching resource directory for a route change**: don't invent a new
  path — re-read `router.go`/`server.go`, the route is mounted somewhere.
