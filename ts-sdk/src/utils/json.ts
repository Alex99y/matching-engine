// The Go API serializes amount/price fields as JSON numbers backed by uint64.
// JavaScript's `number` silently loses integer precision above 2^53, so we
// decode those specific wire fields as `bigint` and re-encode bigints as
// unquoted JSON integers on the way out. Pure functions only — no I/O.

/**
 * Raw wire field names (snake_case, as emitted by the API) that carry uint64
 * values and must round-trip as bigint. Keep in sync with the API structs.
 */
export const BIGINT_WIRE_FIELDS: ReadonlySet<string> = new Set([
  "price",
  "quantity",
  "quote_qty",
  "price_quantum",
  "amount_quantum",
  "min_order_size",
  "max_order_size",
  "have_quantity",
  "want_quantity",
  "remaining_have",
  "remaining_want",
]);

// V8 (Node 21.7+, all of Node 22) passes a third `context` argument to the
// reviver exposing the raw source text of the value. That is the only way to
// recover full uint64 precision, since `value` has already been coerced to a
// lossy `number` by the time the reviver runs.
interface ReviverContext {
  source: string;
}

type BigIntReviver = (
  key: string,
  value: unknown,
  context?: ReviverContext,
) => unknown;

/**
 * Parse JSON, decoding {@link BIGINT_WIRE_FIELDS} as bigint without precision
 * loss. Throws the native SyntaxError on malformed input; callers in http/
 * map that into a ParseError.
 */
export function parseWithBigInts(text: string): unknown {
  const reviver: BigIntReviver = (key, value, context) => {
    if (!BIGINT_WIRE_FIELDS.has(key) || value === null || value === undefined) {
      return value;
    }
    if (context && typeof context.source === "string") {
      return BigInt(context.source);
    }
    // Fallback for runtimes without source-text access: precise up to 2^53.
    if (typeof value === "number" && Number.isInteger(value)) {
      return BigInt(value);
    }
    return value;
  };

  return JSON.parse(
    text,
    reviver as unknown as (key: string, value: unknown) => unknown,
  );
}

// Sentinel wraps bigints during stringify so we can strip the surrounding
// quotes afterwards, emitting them as unquoted JSON integers. Uses only
// printable ASCII (JSON.stringify never escapes it) and an improbable token to
// avoid colliding with legitimate string values.
const SENTINEL_PREFIX = "__SDK_BIGINT__";
const SENTINEL_PATTERN = /"__SDK_BIGINT__(-?\d+)"/g;

/**
 * Stringify a value, encoding any bigint as an unquoted JSON integer.
 * (Plain JSON.stringify throws on bigint.)
 */
export function stringifyWithBigInts(value: unknown): string {
  const json = JSON.stringify(value, (_key, v: unknown) =>
    typeof v === "bigint" ? `${SENTINEL_PREFIX}${v.toString()}` : v,
  );
  return json.replace(SENTINEL_PATTERN, "$1");
}
