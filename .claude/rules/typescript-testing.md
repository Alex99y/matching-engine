---
paths: ["**/*.ts"]
---

# RULE: Testing with Vitest and coverage

## Setup

- Framework: **Vitest** with the `v8` coverage provider.
- Tests live in `*.test.ts`, mirroring the `src/` structure.
- Minimum scripts in `package.json`:
  ```json
  {
    "test": "vitest run",
    "test:watch": "vitest",
    "test:coverage": "vitest run --coverage"
  }
  ```

## Coverage thresholds (mandatory in `vitest.config.ts`)

```ts
import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    coverage: {
      provider: 'v8',
      reporter: ['text', 'lcov'],
      include: ['src/**/*.ts'],
      exclude: ['src/types/**', 'src/index.ts', '**/*.test.ts'],
      thresholds: {
        lines: 90,
        functions: 90,
        statements: 90,
        branches: 85,
      },
    },
  },
});
```

> **Why 90/85:** an SDK that moves money needs high coverage, but demanding 100% leads to trivial tests written just to satisfy the metric. 90% on lines/functions with 85% on branches forces error paths to be covered without penalizing defensive code that is hard to simulate. The threshold may only go up, never down.

## What to test (mandatory minimum)

1. **Every public function exported from `index.ts`** has at least: 1 happy path + 1 error case + 1 edge case.
2. **Every error-handling branch**: timeouts, non-2xx responses, malformed JSON, network errors.
3. **Pure functions** (parsing, signing, serialization, validation): test exhaustively with case tables (`test.each`).
4. **Retries and rate-limiting**: use `vi.useFakeTimers()` — never real `setTimeout` or `sleep` in tests.

## Test quality rules

- **Zero real network.** Mock the HTTP transport (inject `fetch` or mock the `http/` module). Tests must run offline.
- **Deterministic tests.** No dependence on current time, execution order, or shared state. Pin dates with `vi.setSystemTime()`.
- **One behavioral assertion per test** as a guideline; descriptive names: `it('throws RateLimitError when response is 429')`.
- **Don't test internal implementation** (spying on private methods); test the observable contract.
- **CI fails if coverage drops** or if `.skip`/`.only` tests are committed.
