# ui — Amount Values

## Principle

Every amount the API sends is a raw ME quantum `bigint` (uint64 wire format) — never a human
decimal. Every amount a human sees or types must be the opposite: a decimal value, scaled by the
relevant instrument's `decimals`. Mixing the two — displaying a raw value as if it were decimal,
or doing arithmetic across values scaled by different decimals — is the single most common bug
class in this module. (See the `Available: 50 USDT` / `Needs 64,500,000,000 USDT` bug from
2026-08-17 for exactly what this looks like when it goes wrong.)

## Rules

1. **Never display a raw amount directly.** Any `bigint` amount (price, quantity, balance,
   have/want, operation amount) shown to the user goes through `fmtUnits(raw, decimals)` from
   `src/utils/format.ts`. Never call `.toLocaleString()` / `.toString()` on a raw amount for
   display, and never use `fmtBigInt`/`fmtBigIntRaw` for a value that has known decimals.
2. **Never parse a human amount as a bare integer.** Any user-typed amount goes through
   `parseUnits(input, decimals)`. Never feed price/quantity input straight into `parseBigInt` or
   `BigInt()`.
3. **Decimals always come from `Instrument.decimals`** (via `useInstruments`), matched by symbol.
   Never hardcode a decimals constant for a specific asset — instrument decimals are configured
   server-side and can differ per deployment/seed.
4. **Raw amounts scaled by different decimals are not directly comparable or multipliable.** A
   raw price is quote-quanta *per whole base coin*; a raw quantity is base-quanta. Computing a
   notional/cost from them requires normalizing by the base scale, exactly like the matching
   engine's own `quoteAmount()` does server-side
   (`core/internal/orderbook/orderbook.go`):
   ```ts
   // price: raw quote-quanta per whole base coin, quantity: raw base-quanta
   const notionalRaw = (priceRaw * quantityRaw) / 10n ** BigInt(baseDecimals);
   ```
   Do not write `priceRaw * quantityRaw` and treat the result as being on either input's scale —
   it isn't on any single asset's scale at all.
5. **When an amount's decimals/orientation is genuinely unknown, don't guess.** E.g. a filled or
   cancelled order's `have`/`want` legs can't be mapped to base/quote without `side`, which the
   API only returns for orders still open (see the note in `HistoryPage.tsx`). Show `—` or an
   explicit "raw, unscaled" label instead of a decimal number derived from an assumed scale.
