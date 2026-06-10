---
paths: ["ts-sdk/**/*.ts"]
---

# RULE: Dependencies

## Core rule

**`dependencies` in `package.json` must be empty.** Everything the SDK needs at runtime is implemented with native platform APIs. Development tooling goes in `devDependencies`.

## Rationale

An SDK gets installed inside other people's projects: every runtime dependency is attack surface (supply chain), a risk of version conflicts, and extra weight for the consumer. For an exchange, it is also a direct security risk.

## Rules

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
