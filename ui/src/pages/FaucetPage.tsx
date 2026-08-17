import { useEffect, useState } from "react";
import { useAuth } from "../contexts/AuthContext.tsx";
import { useToast } from "../contexts/ToastContext.tsx";
import { useBalances } from "../contexts/BalanceContext.tsx";
import { useInstruments } from "../hooks/useInstruments.ts";
import { AppHeader } from "../components/AppHeader.tsx";
import { SignInRequired } from "../components/SignInRequired.tsx";
import { fmtUnits } from "../utils/format.ts";

// Sandbox faucet: credits a fixed, per-instrument amount (hardcoded server-side
// in api/internal/faucet/service.go — the client can't choose it), no rate
// limit or cap — so there's no amount field, just "pick an instrument and
// request". We don't predict the amount in this UI (e.g. in the button
// label): that value lives only on the server, and duplicating it here would
// silently go stale if it's ever changed there. The success toast reports
// the real credited amount from the response instead.
function FaucetForm() {
  const { client, session } = useAuth();
  const { showToast } = useToast();
  const { balances, refresh: refreshBalances } = useBalances();
  const instruments = useInstruments(client);

  const [symbol, setSymbol] = useState("");
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!symbol && instruments.length > 0) setSymbol(instruments[0]!.symbol);
  }, [instruments, symbol]);

  if (!session) return null;

  const instrument = instruments.find((i) => i.symbol === symbol);
  const currentBalance = balances.find((b) => b.symbol === symbol)?.balance ?? 0n;

  async function requestFunds() {
    if (!symbol) { showToast("Pick an instrument first", "error"); return; }
    setLoading(true);
    try {
      const result = await session!.requestFaucetFunds(symbol);
      const decimals = instruments.find((i) => i.symbol === result.symbol)?.decimals ?? 0;
      showToast(`Credited ${fmtUnits(result.amount, decimals)} ${result.symbol}`, "success");
      void refreshBalances();
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div style={s.card}>
      <div style={s.header}>
        <span style={s.title}>Request Test Funds</span>
        <span style={s.subtitle}>Sandbox faucet — fixed amount per instrument, no limit on requests</span>
      </div>

      <label style={s.label}>
        Instrument
        <select value={symbol} onChange={(e) => setSymbol(e.target.value)}>
          {instruments.map((i) => (
            <option key={i.symbol} value={i.symbol}>{i.symbol} — {i.name}</option>
          ))}
        </select>
      </label>

      <div style={s.balanceRow}>
        <span style={{ color: "var(--text-muted)", fontSize: 11 }}>Current balance</span>
        <span style={{ fontFamily: "var(--font-mono)", fontWeight: 600 }}>
          {instrument ? fmtUnits(currentBalance, instrument.decimals) : "—"} {symbol}
        </span>
      </div>

      <button
        onClick={() => void requestFunds()}
        disabled={loading || !symbol}
        style={s.submitBtn}
      >
        {loading ? "Requesting…" : symbol ? `Request ${symbol}` : "Request funds"}
      </button>
    </div>
  );
}

export function FaucetPage() {
  const { session } = useAuth();

  return (
    <div style={s.shell}>
      <AppHeader />
      <div style={s.content}>
        {!session ? <SignInRequired what="the faucet" /> : <FaucetForm />}
      </div>
    </div>
  );
}

// ── Styles ────────────────────────────────────────────────────────────────

const s = {
  shell: {
    display: "flex",
    flexDirection: "column" as const,
    height: "100%",
    overflow: "hidden",
  },
  content: {
    flex: 1,
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    overflow: "auto",
    padding: 24,
  },
  card: {
    width: "100%",
    maxWidth: 380,
    background: "var(--bg-panel)",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius-lg)",
    padding: 24,
    display: "flex",
    flexDirection: "column" as const,
    gap: 16,
    animation: "fade-in 200ms ease",
  },
  header: {
    display: "flex",
    flexDirection: "column" as const,
    gap: 4,
    paddingBottom: 4,
    borderBottom: "1px solid var(--border-subtle)",
  },
  title: {
    fontSize: 14,
    fontWeight: 700,
  },
  subtitle: {
    fontSize: 11,
    color: "var(--text-muted)",
  },
  label: {
    display: "flex",
    flexDirection: "column" as const,
    gap: 5,
    fontSize: 11,
    fontWeight: 500,
    color: "var(--text-secondary)",
  },
  balanceRow: {
    display: "flex",
    justifyContent: "space-between",
    alignItems: "center",
    padding: "8px 0",
    borderTop: "1px solid var(--border-subtle)",
    borderBottom: "1px solid var(--border-subtle)",
    fontSize: 12,
  },
  submitBtn: {
    padding: "10px 0",
    background: "var(--accent)",
    color: "#fff",
    borderRadius: "var(--radius-sm)",
    fontWeight: 700,
    fontSize: 14,
  } as const,
} as const;
