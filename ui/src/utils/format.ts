// Formatting helpers for display. All values from the API are raw quantum
// units (uint64 as bigint). Where a value's instrument/market decimals are
// known, prefer fmtUnits/parseUnits below over these raw helpers.

export function fmtBigInt(n: bigint): string {
  return n.toLocaleString("en-US");
}

export function fmtBigIntRaw(n: bigint): string {
  return n.toString();
}

// Format unix-second timestamp as HH:MM:SS (local time).
export function fmtTime(unix: number): string {
  return new Date(unix * 1000).toLocaleTimeString();
}

// Format unix-second timestamp as YYYY-MM-DD HH:MM.
export function fmtDateTime(unix: number): string {
  return new Date(unix * 1000).toLocaleString();
}

// Format a unix-second timestamp relative to now, e.g. "in 6 days", "2 hours ago".
export function fmtRelative(unix: number): string {
  const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  const deltaSeconds = unix - Date.now() / 1000;
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ["year", 31_536_000],
    ["day", 86_400],
    ["hour", 3600],
    ["minute", 60],
  ];
  for (const [unit, span] of units) {
    if (Math.abs(deltaSeconds) >= span) {
      return rtf.format(Math.round(deltaSeconds / span), unit);
    }
  }
  return rtf.format(Math.round(deltaSeconds), "second");
}

// Parse a string as bigint; returns undefined when invalid.
export function parseBigInt(s: string): bigint | undefined {
  const trimmed = s.trim();
  if (!/^\d+$/.test(trimmed)) return undefined;
  try {
    return BigInt(trimmed);
  } catch {
    return undefined;
  }
}

// Shorten a UUID to first 8 chars for display.
export function shortId(id: string): string {
  return id.slice(0, 8) + "…";
}

// Convert a raw ME quantum bigint to a human-readable decimal string.
// e.g. fmtUnits(63_448_000_000n, 6) → "63,448" (USDT with 6 decimals)
//      fmtUnits(169_000_000n, 9)     → "0.169"  (BTC with 9 decimals)
export function fmtUnits(raw: bigint, decimals: number): string {
  if (decimals === 0) return raw.toLocaleString("en-US");
  const scale = 10n ** BigInt(decimals);
  const whole = raw / scale;
  const frac  = raw % scale;
  const fracStr  = frac.toString().padStart(decimals, "0");
  const trimmed  = fracStr.replace(/0+$/, ""); // drop trailing zeros
  return trimmed
    ? `${whole.toLocaleString("en-US")}.${trimmed}`
    : whole.toLocaleString("en-US");
}

// Market ref from base/quote symbols.
export function marketRef(baseSymbol: string, quoteSymbol: string): string {
  return `${baseSymbol}-${quoteSymbol}`;
}

// change_percent_24h from GET /markets/prices arrives pre-formatted
// server-side (2 decimals, "-" already included when negative) — it's a
// display string, not a raw amount, so this only adds the "+" a negative
// number already carries for itself.
export function fmtPercentSigned(pct: string): string {
  return pct.startsWith("-") || pct === "0.00" ? pct : `+${pct}`;
}

// A limit order's "have"/"want" legs map to base/quote by side — mirrors
// limitHaveWant() in core/internal/orderbook/orderbook.go:
//   buy:  have = quote (notional), want = base
//   sell: have = base,             want = quote (notional)
// Only meaningful when side is known (i.e. the order is still open — see
// the note in HistoryPage.tsx about filled/cancelled orders).
export function orderLegDecimals(
  side: string,
  baseDecimals: number,
  quoteDecimals: number,
): { haveDecimals: number; wantDecimals: number } {
  return side === "buy"
    ? { haveDecimals: quoteDecimals, wantDecimals: baseDecimals }
    : { haveDecimals: baseDecimals, wantDecimals: quoteDecimals };
}

// Parse a human-entered decimal string into a raw ME quantum bigint, the
// inverse of fmtUnits. Returns undefined for anything that isn't a
// non-negative decimal number, or that has more fractional digits than the
// instrument supports (we don't silently round — that would misrepresent
// what the user typed).
// e.g. parseUnits("63,448", 6)  → 63_448_000_000n  (USDT with 6 decimals)
//      parseUnits("0.169", 9)  → 169_000_000n      (BTC with 9 decimals)
export function parseUnits(input: string, decimals: number): bigint | undefined {
  const trimmed = input.trim().replace(/,/g, "");
  if (trimmed === "") return undefined;

  const match = /^(\d+)(?:\.(\d+))?$/.exec(trimmed);
  if (!match) return undefined;

  const [, wholeStr, fracStr = ""] = match;
  if (!wholeStr || fracStr.length > decimals) return undefined;

  const scale = 10n ** BigInt(decimals);
  const whole = BigInt(wholeStr) * scale;
  const frac = BigInt(fracStr.padEnd(decimals, "0") || "0");
  return whole + frac;
}
