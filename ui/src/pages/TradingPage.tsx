import { useEffect, useState } from "react";
import type { Instrument, Market } from "ts-sdk";
import { MatchingEngineClient } from "ts-sdk";
import { useAuth } from "../contexts/AuthContext.tsx";
import { useToast } from "../contexts/ToastContext.tsx";
import { MarketSelector } from "../components/MarketSelector.tsx";
import { OrderBook } from "../components/OrderBook.tsx";
import { CandleChart } from "../components/CandleChart.tsx";
import { OrderForm } from "../components/OrderForm.tsx";
import { OrderList } from "../components/OrderList.tsx";
import { BalancePanel } from "../components/BalancePanel.tsx";

// ── Inline login panel (shown in the right rail when not authenticated) ───

function LoginPanel() {
  const { client, setSession, disconnect } = useAuth();
  const { showToast } = useToast();
  const [mode, setMode] = useState<"login" | "register">("login");
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!username.trim() || !password.trim()) {
      showToast("Username and password are required", "error");
      return;
    }
    if (mode === "register" && !email.trim()) {
      showToast("Email is required for registration", "error");
      return;
    }
    if (!client) return;

    setLoading(true);
    try {
      if (mode === "register") {
        await client.register({ username: username.trim(), email: email.trim(), password });
        showToast("Account created — logging in…", "success");
      }
      const session = await client.login({ username: username.trim(), password });
      setSession(session, username.trim());
      showToast(`Welcome, ${username.trim()}!`, "success");
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div style={lp.container}>
      <div style={lp.header}>
        <span style={lp.title}>Sign in to trade</span>
      </div>

      <div style={lp.tabs}>
        {(["login", "register"] as const).map((m) => (
          <button
            key={m}
            type="button"
            onClick={() => setMode(m)}
            style={{ ...lp.tab, ...(mode === m ? lp.tabActive : {}) }}
          >
            {m === "login" ? "Login" : "Register"}
          </button>
        ))}
      </div>

      <form onSubmit={handleSubmit} style={lp.form}>
        <label style={lp.label}>
          Username
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="bot1"
            autoComplete="username"
          />
        </label>

        {mode === "register" && (
          <label style={lp.label}>
            Email
            <input
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="bot@exchange.io"
              type="email"
            />
          </label>
        )}

        <label style={lp.label}>
          Password
          <input
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            type="password"
            placeholder="••••••••"
            autoComplete={mode === "login" ? "current-password" : "new-password"}
          />
        </label>

        <button type="submit" disabled={loading} style={lp.submitBtn}>
          {loading
            ? mode === "login" ? "Signing in…" : "Creating account…"
            : mode === "login" ? "Sign in" : "Create account & sign in"}
        </button>
      </form>

      <button type="button" onClick={disconnect} style={lp.disconnectBtn}>
        ← Change server
      </button>
    </div>
  );
}

const lp = {
  container: {
    display: "flex",
    flexDirection: "column" as const,
    gap: 14,
    padding: 16,
    animation: "fade-in 200ms ease",
  },
  header: {
    paddingBottom: 4,
    borderBottom: "1px solid var(--border-subtle)",
  },
  title: {
    fontSize: 11,
    fontWeight: 600,
    textTransform: "uppercase" as const,
    letterSpacing: "0.06em",
    color: "var(--text-secondary)",
  },
  tabs: {
    display: "grid",
    gridTemplateColumns: "1fr 1fr",
    background: "var(--bg-card)",
    borderRadius: "var(--radius)",
    padding: 3,
    gap: 3,
  },
  tab: {
    padding: "6px 0",
    borderRadius: "var(--radius-sm)",
    background: "none",
    color: "var(--text-secondary)",
    fontSize: 12,
    fontWeight: 500,
  } as const,
  tabActive: {
    background: "var(--bg-hover)",
    color: "var(--text-primary)",
    fontWeight: 600,
  } as const,
  form: {
    display: "flex",
    flexDirection: "column" as const,
    gap: 10,
  },
  label: {
    display: "flex",
    flexDirection: "column" as const,
    gap: 4,
    fontSize: 11,
    fontWeight: 500,
    color: "var(--text-secondary)",
  },
  submitBtn: {
    marginTop: 4,
    padding: "9px 0",
    background: "var(--accent)",
    color: "#fff",
    borderRadius: "var(--radius-sm)",
    fontWeight: 700,
    fontSize: 13,
  } as const,
  disconnectBtn: {
    background: "none",
    color: "var(--text-muted)",
    fontSize: 11,
    padding: "4px 0",
    textAlign: "left" as const,
  },
} as const;

// ── Main trading page ─────────────────────────────────────────────────────

export function TradingPage() {
  const { client, session, username, logout, disconnect } = useAuth();
  const { showToast } = useToast();
  const [market, setMarket] = useState("");
  const [selectedMarket, setSelectedMarket] = useState<Market | null>(null);
  const [instruments, setInstruments] = useState<Instrument[]>([]);
  const [orderRefresh, setOrderRefresh] = useState(0);

  if (!(client instanceof MatchingEngineClient)) return null;

  const baseDecimals =
    instruments.find((i) => i.symbol === selectedMarket?.baseSymbol)?.decimals ?? 0;
  const quoteDecimals =
    instruments.find((i) => i.symbol === selectedMarket?.quoteSymbol)?.decimals ?? 0;

  // eslint-disable-next-line react-hooks/rules-of-hooks
  useEffect(() => {
    let active = true;
    client.getInstruments().then((list) => {
      if (active) setInstruments(list);
    }).catch(() => {});
    return () => { active = false; };
  }, [client]);

  async function handleLogout() {
    try {
      await session!.logout();
    } catch {
      // token already expired — ignore
    }
    logout();
    showToast("Signed out", "info");
  }

  return (
    <div style={s.shell}>
      {/* ── Header ─────────────────────────────────────── */}
      <header style={s.header}>
        <div style={s.headerLeft}>
          <span style={s.logo}>⬡ ME</span>
          <MarketSelector value={market} onChange={(ref: string, m: Market) => { setMarket(ref); setSelectedMarket(m); }} />
        </div>

        <div style={s.headerRight}>
          {session ? (
            <>
              <BalancePanel />
              <span style={s.username}>{username}</span>
              <button onClick={() => void handleLogout()} style={s.actionBtn}>
                Sign out
              </button>
            </>
          ) : (
            <span style={s.guestBadge}>Guest — order features disabled</span>
          )}
          <button onClick={disconnect} style={{ ...s.actionBtn, color: "var(--text-muted)" }}>
            ✕
          </button>
        </div>
      </header>

      {/* ── Body ───────────────────────────────────────── */}
      {market ? (
        <div style={s.body}>
          {/* Left: order book */}
          <div style={s.leftPanel}>
            <OrderBook client={client} market={market} quoteDecimals={quoteDecimals} baseDecimals={baseDecimals} />
          </div>

          {/* Centre: chart */}
          <div style={s.centre}>
            <CandleChart client={client} market={market} quoteDecimals={quoteDecimals} />
          </div>

          {/* Right: order panel or login prompt */}
          <div style={s.rightPanel}>
            {session ? (
              <>
                <OrderForm
                  market={market}
                  onOrderPlaced={() => setOrderRefresh((n) => n + 1)}
                />
                <OrderList market={market} refreshSignal={orderRefresh} />
              </>
            ) : (
              <LoginPanel />
            )}
          </div>
        </div>
      ) : (
        <div style={s.empty}>Select a market to start.</div>
      )}
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
  header: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 16,
    padding: "0 16px",
    height: 48,
    background: "var(--bg-panel)",
    borderBottom: "1px solid var(--border)",
    flexShrink: 0,
    zIndex: 10,
  },
  headerLeft: {
    display: "flex",
    alignItems: "center",
    gap: 16,
  },
  headerRight: {
    display: "flex",
    alignItems: "center",
    gap: 16,
  },
  logo: {
    fontSize: 16,
    fontWeight: 700,
    color: "var(--accent)",
    letterSpacing: "-0.02em",
  },
  username: {
    fontSize: 12,
    color: "var(--text-secondary)",
    fontWeight: 500,
  },
  guestBadge: {
    fontSize: 11,
    color: "var(--text-muted)",
    background: "var(--bg-card)",
    padding: "3px 10px",
    borderRadius: "var(--radius-sm)",
    border: "1px solid var(--border)",
  },
  actionBtn: {
    background: "var(--bg-hover)",
    color: "var(--text-secondary)",
    padding: "5px 12px",
    borderRadius: "var(--radius-sm)",
    fontSize: 12,
    fontWeight: 500,
  },
  body: {
    flex: 1,
    display: "grid",
    gridTemplateColumns: "260px 1fr 280px",
    minHeight: 0,
    overflow: "hidden",
  },
  leftPanel: {
    display: "flex",
    flexDirection: "column" as const,
    overflow: "hidden",
    borderRight: "1px solid var(--border)",
  },
  centre: {
    display: "flex",
    flexDirection: "column" as const,
    overflow: "hidden",
  },
  rightPanel: {
    display: "flex",
    flexDirection: "column" as const,
    overflow: "hidden",
    borderLeft: "1px solid var(--border)",
    overflowY: "auto" as const,
  },
  empty: {
    flex: 1,
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    color: "var(--text-muted)",
    fontSize: 14,
  },
} as const;
