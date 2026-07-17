// Formatting helpers for display. All values from the API are raw quantum
// units (uint64 as bigint). We display them as-is since the UI doesn't know
// the per-market decimal configuration — this is a dev/testing tool.

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
