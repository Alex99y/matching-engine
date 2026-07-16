import { useEffect, useState } from "react";
import type { Market } from "ts-sdk";
import { useAuth } from "../contexts/AuthContext.tsx";
import { useToast } from "../contexts/ToastContext.tsx";
import { marketRef } from "../utils/format.ts";
import { Skeleton } from "./Skeleton.tsx";

interface Props {
  value: string;
  onChange: (ref: string, market: Market) => void;
}

export function MarketSelector({ value, onChange }: Props) {
  const { client } = useAuth();
  const { showToast } = useToast();
  const [markets, setMarkets] = useState<Market[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!client) return;
    let active = true;
    client
      .getMarkets()
      .then((list) => {
        if (!active) return;
        setMarkets(list);
        setLoading(false);
        // Auto-select the first market if nothing is selected.
        if (!value && list.length > 0) {
          const first = list[0];
          if (first) onChange(marketRef(first.baseSymbol, first.quoteSymbol), first);
        }
      })
      .catch((err) => {
        if (!active) return;
        setLoading(false);
        showToast(`Failed to load markets: ${err instanceof Error ? err.message : String(err)}`, "error");
      });
    return () => { active = false; };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client]);

  if (loading) return <Skeleton width={120} height={30} />;

  return (
    <select
      value={value}
      onChange={(e) => {
        const ref = e.target.value;
        const market = markets.find(
          (m) => marketRef(m.baseSymbol, m.quoteSymbol) === ref,
        );
        if (market) onChange(ref, market);
      }}
      style={{ width: "auto", minWidth: 130, fontWeight: 600 }}
    >
      {markets.map((m) => {
        const ref = marketRef(m.baseSymbol, m.quoteSymbol);
        return (
          <option key={ref} value={ref}>
            {ref}
          </option>
        );
      })}
    </select>
  );
}
