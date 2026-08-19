---
paths: ["ts-sdk/**/*.ts"]
---

# RULE: Architecture & Distribution

## File and Function Organization

### Base structure

```
src/
  client/        # SDK entry point (main class/factory)
  resources/     # One file per API resource (orders.ts, markets.ts, balances.ts...)
  http/          # Transport: request, retries, rate-limit, request signing
  errors/        # SDK error hierarchy
  types/         # Public types and interfaces (no logic)
  utils/         # Pure, generic helpers (no state, no I/O)
  index.ts       # Single public export point
tests/           # Mirrors the src/ structure (or co-located as *.test.ts)
```

### Rules

1. **One module = one responsibility.** Each file groups functions from a single technical domain. If a file exceeds ~300 lines or mixes responsibilities, split it.
2. **`index.ts` is the only public API.** Anything not exported from `index.ts` is considered internal. Consumers must never import internal paths (`sdk/dist/http/...`).
3. **No circular imports.** If A imports B and B imports A, extract the shared part into a third module (usually `types/` or `utils/`).
4. **Dependency direction flows one way:** `resources → http → utils/types`. Never the other way around (e.g., `http/` must not know about `resources/`).
5. **Pure functions separated from I/O.** Parsing, validation, serialization, and signing live in pure, testable functions; network calls live only in `http/`.
6. **Public types in `types/`, internal types next to their module.** Do not export internal types from `index.ts`.
7. **One named export per concept; avoid `export default`.** It makes refactors, tree-shaking, and autocomplete easier.
8. **File names in `kebab-case.ts`**, consistent with what they export (`rate-limiter.ts` exports `RateLimiter`).
9. **No "misc", "helpers2", "common".** If you don't know where a function belongs, that's a sign a clearly named module is missing.

## Public API Design

### Principle

The SDK's public surface is a contract. Every export is a compatibility promise: export the minimum, name things predictably, and only break in a major version.

### Rules

1. **Single, configurable entry point** — a class or a factory. The few
   required connection parameters may be positional; everything else goes in an
   options object.
   ```ts
   // class form (current SDK)
   const client = new MatchingEngineClient(host, port, {
     timeoutMs,          // explicit default
     maxRetries,         // explicit default
     allowInsecure,      // opt in to http://; https is the default
     fetch,              // injectable (testing / custom environments)
   });

   // factory form is equally acceptable
   const client = createClient({ baseUrl, timeoutMs, fetch });
   ```
   No configuration via global environment variables and no singletons.
2. **Every network method is `async` and returns a typed `Promise<T>`.** No callbacks, no mixed sync/async APIs.
3. **Consistent, predictable naming:** use `getX` for reads (`getOrder` for one, `getOrders`/`listOrders` for many — pick one convention and apply it everywhere), and `createX`, `cancelX` for mutations. Same verb = same semantics across the whole SDK.
4. **Parameters: positional for the few required ones; an options object for the rest.** More than 2 parameters → object.
5. **The SDK prints nothing.** Zero `console.log`. If observability is needed, expose an optional hook (`onRequest`, `onRetry`) or an injectable `logger` that does nothing by default.
6. **No global mutable state.** Two clients created with different configs must coexist without interfering with each other.
7. **Explicit idempotency where it matters:** methods that create orders accept a `clientOrderId`/idempotency key and document it.
8. **Mandatory JSDoc on the entire public surface:** what it does, parameters, return value, `@throws`, and at least one `@example` per resource.
9. **Strict SemVer.** Removing/renaming an export, changing a return type, or adding a required field = major. Adding optionals = minor. Maintain a `CHANGELOG.md`.
10. **A `package.json` ready for publishing:** a defined `exports` map, `types` pointing to the `.d.ts` files, `sideEffects: false`, and `files` limited to the build output dir `build/` (don't publish tests or unnecessary sources).

## Dependencies

### Core rule

**`dependencies` in `package.json` must be empty.** Everything the SDK needs at runtime is implemented with native platform APIs. Development tooling goes in `devDependencies`.

### Rationale

An SDK gets installed inside other people's projects: every runtime dependency is attack surface (supply chain), a risk of version conflicts, and extra weight for the consumer. For an exchange, it is also a direct security risk.

### Rules

1. **Runtime = native APIs only:**
   - HTTP → global `fetch` (Node ≥ 18).
   - Timeouts/cancellation → `AbortController`.
   - Crypto/signing (HMAC, SHA-256) → `node:crypto` or Web Crypto (`globalThis.crypto.subtle`).
   - WebSocket (if applicable) → global `WebSocket` or injected by the consumer, never bundled.
2. **Allowed `devDependencies`:** `typescript`, `vitest`, `@vitest/coverage-v8`, linter/formatter (`eslint`, `prettier`, or `biome`), bundler (`tsup`/`tsdown`), and types (`@types/*`). Keep the list short.
3. **Documented exception or nothing.** If a runtime dependency turns out to be unavoidable, it requires: a written justification in the PR, a review of the dependency's code, an exact version (no `^` or `~`), and ideally zero transitive dependencies.
4. **`peerDependencies` only for optional integrations** (e.g., a specific WS client), never for core functionality.
5. **Lockfile committed** and `npm audit` (or equivalent) in CI; the build fails on high/critical vulnerabilities in dev deps.
6. **Copy-pasting entire libraries into `src/`** to "avoid the dependency" is forbidden: that is worse (unmaintained code). Implement only the minimum needed, owned and tested.
7. **Automated verification:** a test or CI script that fails if `Object.keys(pkg.dependencies ?? {}).length > 0`.
