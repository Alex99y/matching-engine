---
paths: ["**/*.ts"]
---

# RULE: Code

## Strict TypeScript

### Minimum configuration (`tsconfig.json`)

```json
{
  "compilerOptions": {
    "strict": true,
    "noUncheckedIndexedAccess": true,
    "noImplicitOverride": true,
    "exactOptionalPropertyTypes": true,
    "noFallthroughCasesInSwitch": true,
    "verbatimModuleSyntax": true,
    "isolatedModules": true,
    "module": "NodeNext",
    "target": "ES2022",
    "declaration": true,
    "sourceMap": true
  }
}
```

### Rules

1. **`any` is forbidden.** Use `unknown` at the boundaries (HTTP responses, error `cause`) and narrow with type guards or your own parsers. `// @ts-ignore` and `// @ts-expect-error` only with a justifying comment, and always prefer the latter.
2. **Validate at runtime everything that comes from outside.** The type of an HTTP response is a promise, not a guarantee: every response is validated with guard functions (`isOrderResponse(x): x is OrderResponse`) before being typed. If it doesn't validate → `ParseError`.
3. **Monetary numbers are never `number` without an explicit decision.** Prices and amounts on an exchange travel as `string` (or `bigint` for scaled integers) to avoid float precision loss. If `number` is exposed, it must be documented why it is safe.
4. **Explicit types on the public API.** Every exported function declares its return type; no inference on the public surface.
5. **Prefer `readonly` and immutability** in public types (`readonly`, `ReadonlyArray`). The SDK never mutates objects it receives from the consumer.
6. **Discriminated unions over loose booleans** for states (`{ status: 'open' | 'filled' | 'cancelled' }`), with exhaustive `switch` checked via `never`.
7. **No TS enums;** use literal unions or `const` objects (`as const`), which generate less code and are more interoperable.
8. **The type build is part of CI:** `tsc --noEmit` must pass clean; published `.d.ts` files are generated from the code, not written by hand.

## Error Handling

### Principle

The SDK **never** lets raw runtime errors (`TypeError`, `FetchError`, JSON `SyntaxError`) escape to the consumer. Every error that crosses the public API is an instance of the SDK's own hierarchy.

### Minimum hierarchy (in `src/errors/`)

```ts
export class SDKError extends Error {
  constructor(message: string, public readonly cause?: unknown) {
    super(message);
    this.name = new.target.name;
  }
}

export class NetworkError extends SDKError {}        // network failure, DNS, connection refused
export class TimeoutError extends NetworkError {}    // request aborted by timeout
export class APIError extends SDKError {             // non-2xx response from the server
  constructor(message: string, public readonly status: number, public readonly body?: unknown) {
    super(message);
  }
}
export class AuthenticationError extends APIError {} // 401 / 403
export class RateLimitError extends APIError {       // 429
  constructor(message: string, status: number, public readonly retryAfterMs?: number) {
    super(message, status);
  }
}
export class ValidationError extends SDKError {}     // invalid input detected client-side
export class ParseError extends SDKError {}          // response with unexpected shape
```

### Rules

1. **Always map at the boundary.** The `http/` module is the only place where native errors (`fetch`, `AbortError`, `JSON.parse`) are caught and converted into SDK errors. Above that layer, nobody `try/catch`es native errors.
2. **Preserve the cause.** Use the `cause` field so the original stack is not lost. Never swallow an error (empty `catch {}` is forbidden).
3. **Validate inputs before hitting the network.** Missing required parameters, wrong types, or out-of-range values throw `ValidationError` without making the request.
4. **Errors rich in context, poor in secrets.** Include method, endpoint, status, and request-id if available. **Never** include API keys, signatures, or auth headers in messages or in `cause`.
5. **429 and 5xx are retryable; 4xx is not** (unless an explicit policy says otherwise). Retry logic lives in `http/` with exponential backoff + jitter and a configurable maximum number of attempts. When retries are exhausted, throw the last error.
6. **Mandatory timeout on every request** (`AbortController`), with a sensible default that is configurable per call. No timeout = bug.
7. **No `throw`ing strings or plain objects.** Only instances of the hierarchy.
8. **Document what each public method throws** in its JSDoc (`@throws`).
9. **`instanceof` must work** for the consumer: no broken inheritance (verify with a test that `e instanceof RateLimitError && e instanceof APIError && e instanceof SDKError`).

## Testing with Vitest and Coverage

### Setup

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

### Coverage thresholds (mandatory in `vitest.config.ts`)

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

### What to test (mandatory minimum)

1. **Every public function exported from `index.ts`** has at least: 1 happy path + 1 error case + 1 edge case.
2. **Every error-handling branch**: timeouts, non-2xx responses, malformed JSON, network errors.
3. **Pure functions** (parsing, signing, serialization, validation): test exhaustively with case tables (`test.each`).
4. **Retries and rate-limiting**: use `vi.useFakeTimers()` — never real `setTimeout` or `sleep` in tests.

### Test quality rules

- **Zero real network.** Mock the HTTP transport (inject `fetch` or mock the `http/` module). Tests must run offline.
- **Deterministic tests.** No dependence on current time, execution order, or shared state. Pin dates with `vi.setSystemTime()`.
- **One behavioral assertion per test** as a guideline; descriptive names: `it('throws RateLimitError when response is 429')`.
- **Don't test internal implementation** (spying on private methods); test the observable contract.
- **CI fails if coverage drops** or if `.skip`/`.only` tests are committed.
