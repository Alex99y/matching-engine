import type { Instrument, Market } from "ts-sdk";

export interface ScaleFactors {
  readonly priceQuantum: bigint;
  readonly amountQuantum: bigint;
  readonly minOrderSize: bigint;
  readonly maxOrderSize: bigint;
  readonly quoteDecimals: number;
  readonly baseDecimals: number;
}

export function buildScaleFactors(
  market: Market,
  instruments: readonly Instrument[],
): ScaleFactors {
  const base = instruments.find((i) => i.symbol === market.baseSymbol);
  const quote = instruments.find((i) => i.symbol === market.quoteSymbol);
  if (!base) throw new Error(`Instrument not found: ${market.baseSymbol}`);
  if (!quote) throw new Error(`Instrument not found: ${market.quoteSymbol}`);
  return {
    priceQuantum:  market.priceQuantum,
    amountQuantum: market.amountQuantum,
    minOrderSize:  market.minOrderSize,
    maxOrderSize:  market.maxOrderSize,
    quoteDecimals: quote.decimals,
    baseDecimals:  base.decimals,
  };
}

function snapToGrid(value: bigint, quantum: bigint): bigint {
  if (quantum === 0n) return value;
  return (value / quantum) * quantum;
}

// Parse a Binance decimal string (e.g. "67543.21") to integer units using
// pure bigint arithmetic — avoids float precision loss entirely.
function decimalToUnits(s: string, decimals: number): bigint {
  const dot = s.indexOf(".");
  const intStr  = dot === -1 ? s : s.slice(0, dot);
  const fracStr = dot === -1 ? "" : s.slice(dot + 1);
  const padded  = fracStr.padEnd(decimals, "0").slice(0, decimals);
  return BigInt(intStr || "0") * (10n ** BigInt(decimals)) + BigInt(padded || "0");
}

/**
 * Convert a Binance string price to ME price quantum units.
 * Returns null when the result is zero after snapping (price too small for ME).
 */
export function toMePrice(binancePrice: string, scale: ScaleFactors): bigint | null {
  const raw     = decimalToUnits(binancePrice, scale.quoteDecimals);
  const snapped = snapToGrid(raw, scale.priceQuantum);
  return snapped > 0n ? snapped : null;
}

/**
 * Convert a Binance string quantity to ME amount quantum units.
 * Returns null when the quantity falls outside the market's min/max order size.
 */
export function toMeQty(binanceQty: string, scale: ScaleFactors): bigint | null {
  const raw     = decimalToUnits(binanceQty, scale.baseDecimals);
  const snapped = snapToGrid(raw, scale.amountQuantum);
  if (snapped < scale.minOrderSize || snapped > scale.maxOrderSize) return null;
  return snapped;
}

// ME stores notional as price×qty/baseScale in a BIGINT column — hard ceiling is int64 max.
const MAX_STORABLE = 9_223_372_036_854_775_807n; // math.MaxInt64

/**
 * Cap qty so that (price×qty/baseScale) stays within the ME's storable maximum (int64 max).
 * baseScale = 10^baseDecimals normalises the product back to quote-quanta; without it the
 * cap was 10^baseDecimals times too tight (e.g. 0.144 BTC instead of ~144M BTC at $64k).
 * Returns null if the capped value falls below the market's minOrderSize.
 */
export function capQtyToNotional(price: bigint, qty: bigint, scale: ScaleFactors): bigint | null {
  if (price === 0n) return null;
  const baseScale = 10n ** BigInt(scale.baseDecimals);
  const maxByNotional = (MAX_STORABLE * baseScale) / price; // bigint floor division
  if (qty <= maxByNotional) return qty;                      // already within limit
  const capped = snapToGrid(maxByNotional, scale.amountQuantum);
  if (capped < scale.minOrderSize) return null;
  return capped;
}
