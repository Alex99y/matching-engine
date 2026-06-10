---
name: sync-sdk-from-go
description: Synchronizes the TypeScript SDK under `ts-sdk/` whenever a feature changes in the Go API under `api/` — new or changed endpoints, request/response structs, validation rules, routes, or error cases. Call this skill manually after editing any `api/internal/<resource>/{handler,service,router}.go` or `api/internal/server/server.go`. It reads the full contract across the Go layers, diffs it against the SDK, applies the changes (types, resource methods, validation, response parsing, bigint wire fields, clients, tests), updates `CHANGELOG.md`, and validates with tsc + vitest. Use it any time an API feature lands and you want the SDK to stay in sync.
---

# Sync SDK from the Go API

Keeps `ts-sdk/` in sync with **features** shipped in the Go API — not just type
edits. A feature can change types, validation, routes, or error behavior in any
combination (e.g. moving `/order` → `/api/v1/order` changed a route and **no**
types at all). Run this skill manually after an API change.

> ⚠️ A pure type-diff is not enough. Routes and validation rules live in
> different files than the structs, and amount fields need a matching entry in
> the SDK's bigint wire-field set or they silently lose precision. Always walk
> the layer checklist below.

## Source-of-truth layers → SDK targets

The Go files are **singular** (`handler.go`, `service.go`, `router.go`).

| Go source (truth) | What to extract | SDK target |
|---|---|---|
| `api/internal/<r>/handler.go` | request/response structs, `json:` tags, `validate:` tags, HTTP status codes per error | `ts-sdk/src/types/index.ts`, `ts-sdk/src/resources/<r>.ts`, `ts-sdk/src/utils/parse.ts`, `ts-sdk/src/utils/validation.ts` |
| `api/internal/<r>/service.go` | package-level error vars (e.g. `ErrInvalidLimit`), business rules (limits, defaults) | `ts-sdk/src/utils/validation.ts`, `@throws` JSDoc, error mapping |
| `api/internal/<r>/router.go` | the resource group prefix + per-route method/path | resource path constant in `ts-sdk/src/resources/<r>.ts` |
| `api/internal/server/server.go` | the parent group prefix that the router is mounted under | full route path (see Step 3) |
| any uint64 amount field | the `json:` wire name | `BIGINT_WIRE_FIELDS` in `ts-sdk/src/utils/json.ts` |
| any of the above | — | co-located `*.test.ts`, `test/e2e.spec.ts`, `CHANGELOG.md` |

---

## Step 1 — Identify scope

Find what changed in the API. Prefer git over guessing:

```bash
git status --porcelain api/ ; git diff --stat HEAD -- api/
```

Determine the affected resource(s) `<r>` (e.g. `orders`, `markets`). For each,
read **all** of its layer files plus the server, in full:

```bash
ls api/internal/<r>/            # handler.go service.go router.go
# read: api/internal/<r>/handler.go, service.go, router.go
# read: api/internal/server/server.go
```

---

## Step 2 — Extract the contract

From **handler.go**:
- **Request structs** (`c.Bind().Body(&req)`) → method params / request body.
- **Response structs** returned as JSON → `Promise<T>` return types.
- For each field: the **`json:` tag name** (not the Go field name), the type
  (mapping below), whether it is a pointer (`*T` → optional), and any
  `validate:` tag (e.g. `min=32,max=64`).
- The **HTTP status** each `errors.Is` branch maps to (e.g. 404, 409, 422) →
  feeds `@throws` docs and confirms error-class mapping.

From **service.go**:
- Package-level `Err*` variables and the rules behind them (e.g. `limit` must be
  1–100, default 10). These are mirrored client-side as `ValidationError`s.

From **router.go + server.go** (route path — see Step 3).

### Go → TypeScript type mapping

| Go type | TypeScript | Notes |
|---|---|---|
| `string` | `string` | |
| `int`, `int32` | `number` | e.g. `decimals` |
| `uint64`, `int64` **(amount)** | `bigint` | prices, quantities, quantums, sizes, remaining_* |
| `int64` **(timestamp)** | `number` | `*_at` unix-seconds fields (`created_at`, `expires_at`, `cancelled_at`) |
| `float32`, `float64` | `number` | avoid for money |
| `bool` | `boolean` | |
| `*T` (pointer) | optional `?: T` | omit when undefined; never send `null` |
| `[]T` | `T[]` | |
| `time.Time` | `string` | RFC3339 (e.g. instrument `created_at`) |
| `const` string group | string-literal union via `as const` object | no TS `enum` |

> **Amount rule (this SDK uses `bigint`, not `string`):** any uint64/int64 field
> representing money or size — name contains `price`, `quantity`, `qty`,
> `amount`, `quantum`, `size`, `remaining`, `have`, `want`, `fee`, `balance` —
> maps to **`bigint`**. The transport serializes/parses bigint losslessly.
> **int64 timestamps are the exception → `number`.** If unsure whether an int64
> is money or a timestamp, check the field name/`_at` suffix and flag it.

> **Critical:** when you add a new `bigint` field, you **must** add its `json:`
> wire name to `BIGINT_WIRE_FIELDS` in `ts-sdk/src/utils/json.ts`, or the
> reviver will decode it as a lossy `number`.

---

## Step 3 — Derive the full route path

Paths are composed across two files; never read the path off the handler alone.

1. In `server.go`, find the parent group and how the resource router is mounted:
   `apiV1 := app.Group("/api/v1")` then `<r>.Register<R>Routes(apiV1, …)`.
2. In `router.go`, find the resource group and route: `app.Group("/order")` +
   `.Get("/:id", …)`.
3. Full path = parent prefix + resource group + route, e.g.
   `/api/v1` + `/order` + `/:id` = `/api/v1/order/:id`.

Mirror the resource group prefix in the SDK's path constant (e.g.
`const ORDERS_BASE = "/api/v1/order"`). A route move with no type change still
requires this update.

---

## Step 4 — Diff and plan

Build an explicit change list before editing. Cover every category that applies:

```
CHANGE 1: [file] — [what changes and why]
```

- **Added field** → TS interface + `parse.ts` guard (+ `BIGINT_WIRE_FIELDS` if
  bigint) + JSDoc + test coverage.
- **Removed / renamed field** → interface, `parse.ts`, tests, examples. Breaking.
- **Type changed** → TS type, `parse.ts` accessor, `BIGINT_WIRE_FIELDS` membership.
- **New validation rule** → `validation.ts` + `@throws` + test.
- **Route added/changed/moved** → path constant + resource method + tests + e2e
  mock routes.
- **New endpoint** → end-to-end: types, `parse.ts`, `validation.ts`, resource
  function, client method (+ JSDoc with `@example`), happy-path + error tests,
  e2e route.
- **New error status** → confirm transport maps it (401/403→Authentication,
  429→RateLimit, others→APIError) and document `@throws`.
- **Deleted endpoint** → remove method + types + tests. Breaking.

Show the plan and **ask for confirmation before applying any breaking change**
(removed/renamed field, removed endpoint, type narrowing, method rename).

---

## Step 5 — Apply

File by file; change only what the plan lists, no unrelated refactors.

- Public types are **camelCase**; wire is **snake_case**. The mapping lives in
  `parse.ts` — update both the interface (camelCase) and the guard (reads the
  snake_case key).
- Keep existing JSDoc; touch only the affected `@param`/`@returns`/`@throws`/
  `@example` lines. New public methods need full JSDoc incl. one `@example`.
- Tests are **co-located** (`src/resources/<r>.test.ts`, etc.); integration
  lives in `test/e2e.spec.ts`. Use bigint literals (`5n`) in fixtures.
- Add a test case for every new method (≥1 happy path + 1 error). Do not delete
  existing tests unless the endpoint was removed.

---

## Step 6 — Update the changelog

Append to the `## [Unreleased]` section of `ts-sdk/CHANGELOG.md` (create that
section if missing). Classify each entry:

- **Added** — new endpoints, methods, fields, options.
- **Changed** — altered behavior/signatures (route moves go here).
- **Removed** — deleted surface (breaking).
- **Fixed** — bug fixes.

Do **not** pick a version number or release date and do **not** bump
`package.json` — cutting a release is a separate human step. If the change is
breaking, note it explicitly under the entry so the next release is tagged a
major bump (per api-design SemVer rule).

---

## Step 7 — Validate

```bash
cd ts-sdk
npm run typecheck          # tsc --noEmit, must be clean
npm run test:coverage      # vitest; thresholds 90 lines/fns/stmts, 85 branches
npm run build              # confirm emit
```

Fix all type errors. If coverage dropped below threshold, add the missing tests
(don't lower the threshold).

---

## Step 8 — Report

```
## SDK sync complete

Source: api/internal/<r>/{handler,service,router}.go (+ server.go)
Full route(s): <METHOD path>
Changes applied:
- ts-sdk/src/types/index.ts        — <what>
- ts-sdk/src/resources/<r>.ts      — <what>
- ts-sdk/src/utils/parse.ts        — <what>
- ts-sdk/src/utils/validation.ts   — <what>
- ts-sdk/src/utils/json.ts         — <BIGINT_WIRE_FIELDS additions, if any>
- ts-sdk/src/<...>.test.ts          — <what>
- ts-sdk/CHANGELOG.md              — [Unreleased] entries

Breaking changes: none | <list>
Typecheck: pass | <errors>
Tests: <n> passed | <failures>
Coverage: <lines>% lines / <branches>% branches
```

---

## Edge cases

- **Struct embedding (`type Foo struct { Bar }`):** flatten embedded fields into
  the TS interface.
- **`const` string groups in Go:** convert to a `const` object + string-literal
  union (`OrderSide`, `OrderType`, `TimeInForce`); never a TS `enum`.
- **New bigint field forgotten in `BIGINT_WIRE_FIELDS`:** the symptom is a
  number where a bigint is expected — always cross-check this set.
- **Route move with no type change:** still update the path constant, the
  resource tests, and the e2e mock routes — the most common silent break.
- **Several resources changed at once:** process each in sequence, accumulate
  changes, run validation once at the end.
- **No matching SDK resource found:** do not scaffold a new resource silently —
  flag it and ask whether to create the module (mirroring an existing resource).
