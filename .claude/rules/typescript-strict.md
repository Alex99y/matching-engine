---
paths: ["**/*.ts"]
---

# RULE: Strict TypeScript

## Minimum configuration (`tsconfig.json`)

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

## Rules

1. **`any` is forbidden.** Use `unknown` at the boundaries (HTTP responses, error `cause`) and narrow with type guards or your own parsers. `// @ts-ignore` and `// @ts-expect-error` only with a justifying comment, and always prefer the latter.
2. **Validate at runtime everything that comes from outside.** The type of an HTTP response is a promise, not a guarantee: every response is validated with guard functions (`isOrderResponse(x): x is OrderResponse`) before being typed. If it doesn't validate → `ParseError`.
3. **Monetary numbers are never `number` without an explicit decision.** Prices and amounts on an exchange travel as `string` (or `bigint` for scaled integers) to avoid float precision loss. If `number` is exposed, it must be documented why it is safe.
4. **Explicit types on the public API.** Every exported function declares its return type; no inference on the public surface.
5. **Prefer `readonly` and immutability** in public types (`readonly`, `ReadonlyArray`). The SDK never mutates objects it receives from the consumer.
6. **Discriminated unions over loose booleans** for states (`{ status: 'open' | 'filled' | 'cancelled' }`), with exhaustive `switch` checked via `never`.
7. **No TS enums;** use literal unions or `const` objects (`as const`), which generate less code and are more interoperable.
8. **The type build is part of CI:** `tsc --noEmit` must pass clean; published `.d.ts` files are generated from the code, not written by hand.
