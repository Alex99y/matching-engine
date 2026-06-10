---
paths: ["**/*.ts"]
---

# RULE: Technical security

> Code-level security rules for the SDK, independent of the exchange's business logic.

## Rules

1. **Secrets never touch disk or logs.** API keys and secrets live only in memory inside the client. Forbidden: including them in error messages, in the client's `toString()`/`JSON.stringify()`, in logging hooks, or in URLs (always in headers/signed body).
2. **Redact on serialization.** If the config object is inspectable, implement redaction (`apiSecret: '[REDACTED]'`) in `toJSON` and in `node:util.inspect.custom`.
3. **Signatures with native crypto and constant-time comparison.** HMAC via `node:crypto`/Web Crypto; any comparison of signatures or tokens uses `timingSafeEqual`, never `===`.
4. **HTTPS required.** Reject `baseUrl` with `http://` unless an explicit flag like `allowInsecure: true` is set, intended only for local tests and documented as dangerous.
5. **Safe URL and query construction:** always `URL` / `URLSearchParams`, never string concatenation with user input.
6. **Clocks and nonces:** if signing uses a timestamp/nonce, generate it in a single, testable, injectable module; handle the "timestamp outside window" error as an explicit case.
7. **Don't trust the server's response:** validate the structure before using it (see the strict TypeScript rule); limit the accepted body size to avoid unbounded memory consumption.
8. **Public error messages without internal information:** no server stack traces or local file paths in errors re-thrown to the consumer (the full cause goes in `cause`, not in `message`).
9. **CI with auditing:** `npm audit` and basic security linting; `eval`, `new Function`, and dynamic `require`/`import` with variable input are forbidden.
