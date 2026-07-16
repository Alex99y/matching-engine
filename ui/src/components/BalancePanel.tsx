import { useCallback, useEffect, useState } from "react";
import type { Balance } from "ts-sdk";
import { useSession } from "../contexts/AuthContext.tsx";
import { useToast } from "../contexts/ToastContext.tsx";
import { Skeleton } from "./Skeleton.tsx";

export function BalancePanel() {
  const { session } = useSession();
  const { showToast } = useToast();
  const [balances, setBalances] = useState<Balance[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchBalances = useCallback(async () => {
    try {
      const list = await session.getBalances();
      setBalances(list);
    } catch (err) {
      showToast(
        `Failed to load balances: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
    } finally {
      setLoading(false);
    }
  }, [session, showToast]);

  useEffect(() => {
    void fetchBalances();
  }, [fetchBalances]);

  if (loading) {
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
        <div key={b.symbol} style={s.item} title={`blocked: ${b.blocked.toLocaleString()}`}>
          <span style={s.symbol}>{b.symbol}</span>
          <span style={s.amount}>{b.balance.toLocaleString()}</span>
          {b.blocked > 0n && (
            <span style={s.blocked}>−{b.blocked.toLocaleString()}</span>
          )}
        </div>
      ))}
      <button onClick={() => void fetchBalances()} style={s.refreshBtn} title="Refresh balances">
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
