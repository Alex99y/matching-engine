import { useEffect, useState } from "react";
import type { Market, MatchingEngineClient } from "ts-sdk";
import { useInstruments } from "../hooks/useInstruments.ts";
import { useMarketPrices } from "../hooks/useMarketPrices.ts";
import { fmtPercentSigned, fmtUnits, marketRef } from "../utils/format.ts";
import { Skeleton } from "./Skeleton.tsx";

interface Props {
  client: MatchingEngineClient;
  activeMarket: string;
  onSelect: (ref: string, market: Market) => void;
}

// Horizontal strip of every market's last price and 24h change, between the
// header and the trading panels. Driven by the markets list (not the prices
// list) so a market with no trades yet still gets a tile — just with "—"
// where its price would be — instead of silently not appearing.
export function PriceTicker({ client, activeMarket, onSelect }: Props) {
  const [markets, setMarkets] = useState<Market[]>([]);
  const [loading, setLoading] = useState(true);
  const instruments = useInstruments(client);
  const prices = useMarketPrices(client);

  useEffect(() => {
    let active = true;
    client.getMarkets().then((list) => {
      if (active) { setMarkets(list); setLoading(false); }
    }).catch(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [client]);

  if (loading) {
    return (
      <div style={s.bar}>
        {Array.from({ length: 4 }, (_, i) => <Skeleton key={i} width={110} height={32} />)}
      </div>
    );
  }

  return (
    <div style={s.bar}>
      {markets.map((m) => {
        const ref = marketRef(m.baseSymbol, m.quoteSymbol);
        const price = prices.find((p) => p.market === ref);
        const quoteDecimals = instruments.find((i) => i.symbol === m.quoteSymbol)?.decimals;

        const priceLabel =
          price?.price !== undefined && quoteDecimals !== undefined
            ? fmtUnits(price.price, quoteDecimals)
            : "—";
        const change = price?.changePercent24h;
        const changeColor = change === undefined ? "var(--text-muted)"
          : change.startsWith("-") ? "var(--red)"
          : change === "0.00" ? "var(--text-muted)"
          : "var(--green)";

        return (
          <button
            key={ref}
            onClick={() => onSelect(ref, m)}
            style={{ ...s.item, ...(ref === activeMarket ? s.itemActive : {}) }}
          >
            <span style={s.symbol}>{ref}</span>
            <span style={s.price}>{priceLabel}</span>
            <span style={{ ...s.change, color: changeColor }}>
              {change !== undefined ? `${fmtPercentSigned(change)}%` : "—"}
            </span>
          </button>
        );
      })}
    </div>
  );
}

// ── Styles ────────────────────────────────────────────────────────────────

const s = {
  bar: {
    display: "flex",
    alignItems: "center",
    gap: 6,
    padding: "6px 12px",
    background: "var(--bg-panel)",
    borderBottom: "1px solid var(--border)",
    overflowX: "auto" as const,
    flexShrink: 0,
  },
  item: {
    display: "flex",
    alignItems: "baseline",
    gap: 7,
    padding: "5px 10px",
    borderRadius: "var(--radius-sm)",
    background: "none",
    whiteSpace: "nowrap" as const,
    flexShrink: 0,
  },
  itemActive: {
    background: "var(--bg-hover)",
  },
  symbol: {
    fontSize: 11,
    fontWeight: 700,
    color: "var(--text-primary)",
  },
  price: {
    fontSize: 12,
    fontFamily: "var(--font-mono)",
    color: "var(--text-secondary)",
  },
  change: {
    fontSize: 11,
    fontFamily: "var(--font-mono)",
    fontWeight: 600,
  },
} as const;
