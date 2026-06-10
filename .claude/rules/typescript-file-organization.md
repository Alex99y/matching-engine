---
paths: ["ts-sdk/**/*.ts"]
---

# RULE: File and function organization

## Base structure

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

## Rules

1. **One module = one responsibility.** Each file groups functions from a single technical domain. If a file exceeds ~300 lines or mixes responsibilities, split it.
2. **`index.ts` is the only public API.** Anything not exported from `index.ts` is considered internal. Consumers must never import internal paths (`sdk/dist/http/...`).
3. **No circular imports.** If A imports B and B imports A, extract the shared part into a third module (usually `types/` or `utils/`).
4. **Dependency direction flows one way:** `resources → http → utils/types`. Never the other way around (e.g., `http/` must not know about `resources/`).
5. **Pure functions separated from I/O.** Parsing, validation, serialization, and signing live in pure, testable functions; network calls live only in `http/`.
6. **Public types in `types/`, internal types next to their module.** Do not export internal types from `index.ts`.
7. **One named export per concept; avoid `export default`.** It makes refactors, tree-shaking, and autocomplete easier.
8. **File names in `kebab-case.ts`**, consistent with what they export (`rate-limiter.ts` exports `RateLimiter`).
9. **No "misc", "helpers2", "common".** If you don't know where a function belongs, that's a sign a clearly named module is missing.
