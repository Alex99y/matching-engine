---
paths: ["ts-sdk/**/*.ts"]
---

# RULE: Public API design

## Principle

The SDK's public surface is a contract. Every export is a compatibility promise: export the minimum, name things predictably, and only break in a major version.

## Rules

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
