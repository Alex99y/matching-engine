---
paths: ["**/*.ts"]
---

# RULE: Error handling

## Principle

The SDK **never** lets raw runtime errors (`TypeError`, `FetchError`, JSON `SyntaxError`) escape to the consumer. Every error that crosses the public API is an instance of the SDK's own hierarchy.

## Minimum hierarchy (in `src/errors/`)

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

## Rules

1. **Always map at the boundary.** The `http/` module is the only place where native errors (`fetch`, `AbortError`, `JSON.parse`) are caught and converted into SDK errors. Above that layer, nobody `try/catch`es native errors.
2. **Preserve the cause.** Use the `cause` field so the original stack is not lost. Never swallow an error (empty `catch {}` is forbidden).
3. **Validate inputs before hitting the network.** Missing required parameters, wrong types, or out-of-range values throw `ValidationError` without making the request.
4. **Errors rich in context, poor in secrets.** Include method, endpoint, status, and request-id if available. **Never** include API keys, signatures, or auth headers in messages or in `cause`.
5. **429 and 5xx are retryable; 4xx is not** (unless an explicit policy says otherwise). Retry logic lives in `http/` with exponential backoff + jitter and a configurable maximum number of attempts. When retries are exhausted, throw the last error.
6. **Mandatory timeout on every request** (`AbortController`), with a sensible default that is configurable per call. No timeout = bug.
7. **No `throw`ing strings or plain objects.** Only instances of the hierarchy.
8. **Document what each public method throws** in its JSDoc (`@throws`).
9. **`instanceof` must work** for the consumer: no broken inheritance (verify with a test that `e instanceof RateLimitError && e instanceof APIError && e instanceof SDKError`).
