import { describe, expect, it } from "vitest";
import { parseWithBigInts, stringifyWithBigInts } from "./json.js";

describe("parseWithBigInts", () => {
  it("decodes flagged fields as bigint", () => {
    const out = parseWithBigInts('{"price":42,"side":"buy"}') as {
      price: bigint;
      side: string;
    };
    expect(out.price).toBe(42n);
    expect(out.side).toBe("buy");
  });

  it("preserves integer precision above 2^53", () => {
    // 2^53 + 1 cannot be represented exactly as a JS number.
    const out = parseWithBigInts('{"quantity":9007199254740993}') as {
      quantity: bigint;
    };
    expect(out.quantity).toBe(9007199254740993n);
  });

  it("leaves non-flagged numeric fields as number", () => {
    const out = parseWithBigInts('{"decimals":8}') as { decimals: number };
    expect(out.decimals).toBe(8);
    expect(typeof out.decimals).toBe("number");
  });

  it("passes through null for flagged fields", () => {
    const out = parseWithBigInts('{"price":null}') as { price: unknown };
    expect(out.price).toBeNull();
  });

  it("revives bigint fields inside nested arrays", () => {
    const out = parseWithBigInts('[{"remaining_have":10},{"remaining_have":20}]') as Array<{
      remaining_have: bigint;
    }>;
    expect(out[0]?.remaining_have).toBe(10n);
    expect(out[1]?.remaining_have).toBe(20n);
  });
});

describe("stringifyWithBigInts", () => {
  it("emits bigint as an unquoted JSON integer", () => {
    expect(stringifyWithBigInts({ price: 100n })).toBe('{"price":100}');
  });

  it("preserves precision for large bigints on the wire", () => {
    expect(stringifyWithBigInts({ quantity: 9007199254740993n })).toBe(
      '{"quantity":9007199254740993}',
    );
  });

  it("leaves regular values untouched", () => {
    expect(stringifyWithBigInts({ market: "ETH-USDT", limit: 10 })).toBe(
      '{"market":"ETH-USDT","limit":10}',
    );
  });

  it("round-trips a bigint through stringify and parse", () => {
    const json = stringifyWithBigInts({ price: 123456789012345678n });
    const out = parseWithBigInts(json) as { price: bigint };
    expect(out.price).toBe(123456789012345678n);
  });
});
