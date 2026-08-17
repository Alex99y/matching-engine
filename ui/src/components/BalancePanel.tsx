import { useBalances } from "../contexts/BalanceContext.tsx";
import { fmtUnits } from "../utils/format.ts";
import { Skeleton } from "./Skeleton.tsx";

export function BalancePanel() {
  const { balances, loading, refresh } = useBalances();

  if (loading && balances.length === 0) {
    return (
      <div style={s.container}>
        <Skeleton width={80} height={20} />
        <Skeleton width={60} height={20} />
      </div>
    );
  }

  return (
    <div style={s.container}>
      {balances.map((b) => (
        <div
          key={b.symbol}
          style={s.item}
          title={`blocked: ${fmtUnits(b.blocked, b.decimals)}${b.frozen > 0n ? `, frozen: ${fmtUnits(b.frozen, b.decimals)}` : ""}`}
        >
          <span style={s.symbol}>{b.symbol}</span>
          <span style={s.amount}>{fmtUnits(b.balance, b.decimals)}</span>
          {b.blocked > 0n && (
            <span style={s.blocked}>−{fmtUnits(b.blocked, b.decimals)}</span>
          )}
        </div>
      ))}
      <button onClick={() => void refresh()} style={s.refreshBtn} title="Refresh balances">
        ↻
      </button>
    </div>
  );
}

// ── Styles ────────────────────────────────────────────────────────────────

const s = {
  container: {
    display: "flex",
    alignItems: "center",
    gap: 16,
    flexWrap: "wrap" as const,
    animation: "fade-in 200ms ease",
  },
  item: {
    display: "flex",
    alignItems: "baseline",
    gap: 5,
  },
  symbol: {
    fontSize: 10,
    fontWeight: 700,
    textTransform: "uppercase" as const,
    letterSpacing: "0.06em",
    color: "var(--text-secondary)",
  },
  amount: {
    fontSize: 13,
    fontFamily: "var(--font-mono)",
    fontWeight: 600,
  },
  blocked: {
    fontSize: 10,
    color: "var(--red)",
    fontFamily: "var(--font-mono)",
  },
  refreshBtn: {
    background: "none",
    color: "var(--text-muted)",
    fontSize: 14,
    padding: "0 4px",
  },
} as const;
