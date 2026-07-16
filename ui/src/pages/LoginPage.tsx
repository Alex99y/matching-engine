import { useState } from "react";
import { MatchingEngineClient } from "ts-sdk";
import { useAuth } from "../contexts/AuthContext.tsx";
import { useToast } from "../contexts/ToastContext.tsx";
import { DEFAULT_HOST, DEFAULT_PORT, DEFAULT_INSECURE } from "../config.ts";

type Mode = "login" | "register";

export function LoginPage() {
  const { setClient, setSession } = useAuth();
  const { showToast } = useToast();

  // Connection
  const [host, setHost] = useState(DEFAULT_HOST);
  const [port, setPort] = useState(String(DEFAULT_PORT));
  const [insecure, setInsecure] = useState(DEFAULT_INSECURE);

  // Credentials
  const [mode, setMode] = useState<Mode>("login");
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);

  function buildClient(): MatchingEngineClient | null {
    const portNum = parseInt(port, 10);
    if (!portNum || portNum < 1 || portNum > 65535) {
      showToast("Port must be 1–65535", "error");
      return null;
    }
    try {
      return new MatchingEngineClient(host.trim(), portNum, { allowInsecure: insecure });
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
      return null;
    }
  }

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

    const client = buildClient();
    if (!client) return;

    setLoading(true);
    try {
      if (mode === "register") {
        await client.register({ username: username.trim(), email: email.trim(), password });
        showToast("Account created — logging in…", "success");
      }
      const session = await client.login({ username: username.trim(), password });
      setClient(client);
      setSession(session, username.trim());
      showToast(`Welcome, ${username.trim()}!`, "success");
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setLoading(false);
    }
  }

  function handleBrowseAsGuest() {
    const client = buildClient();
    if (!client) return;
    setClient(client);
    showToast("Browsing as guest — order features require login", "info");
  }

  return (
    <div style={s.page}>
      <div style={s.card}>
        {/* Logo */}
        <div style={s.logo}>
          <span style={s.logoIcon}>⬡</span>
          <span style={s.logoText}>Matching Engine</span>
        </div>

        {/* Server settings */}
        <fieldset style={s.fieldset}>
          <legend style={s.legend}>Server</legend>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 80px", gap: 8 }}>
            <label style={s.label}>
              Host
              <input
                value={host}
                onChange={(e) => setHost(e.target.value)}
                placeholder="localhost"
                autoComplete="off"
              />
            </label>
            <label style={s.label}>
              Port
              <input
                value={port}
                onChange={(e) => setPort(e.target.value)}
                placeholder="4000"
                type="number"
                min={1}
                max={65535}
              />
            </label>
          </div>
          <label style={s.checkRow}>
            <input
              type="checkbox"
              checked={insecure}
              onChange={(e) => setInsecure(e.target.checked)}
              style={{ width: "auto" }}
            />
            <span style={{ color: "var(--text-secondary)", fontSize: 12 }}>
              Allow plain HTTP (local dev)
            </span>
          </label>
        </fieldset>

        {/* Mode tabs */}
        <div style={s.tabs}>
          {(["login", "register"] as Mode[]).map((m) => (
            <button
              key={m}
              type="button"
              onClick={() => setMode(m)}
              style={{ ...s.tab, ...(mode === m ? s.tabActive : {}) }}
            >
              {m === "login" ? "Login" : "Register"}
            </button>
          ))}
        </div>

        {/* Credential form */}
        <form onSubmit={handleSubmit} style={s.form}>
          <label style={s.label}>
            Username
            <input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="bot1"
              autoComplete="username"
              autoFocus
            />
          </label>

          {mode === "register" && (
            <label style={s.label}>
              Email
              <input
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="bot@exchange.io"
                type="email"
                autoComplete="email"
              />
            </label>
          )}

          <label style={s.label}>
            Password
            <input
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              type="password"
              placeholder="••••••••"
              autoComplete={mode === "login" ? "current-password" : "new-password"}
            />
          </label>

          <button type="submit" disabled={loading} style={s.submitBtn}>
            {loading
              ? mode === "login" ? "Signing in…" : "Creating account…"
              : mode === "login" ? "Sign in" : "Create account & sign in"}
          </button>
        </form>

        {/* Guest access */}
        <div style={s.divider}>
          <span style={s.dividerLine} />
          <span style={s.dividerLabel}>or</span>
          <span style={s.dividerLine} />
        </div>
        <button type="button" onClick={handleBrowseAsGuest} style={s.guestBtn}>
          Browse without account
        </button>
      </div>
    </div>
  );
}

// ── Styles ────────────────────────────────────────────────────────────────

const s = {
  page: {
    minHeight: "100%",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    padding: 24,
    background: "var(--bg-base)",
  },
  card: {
    width: "100%",
    maxWidth: 400,
    background: "var(--bg-panel)",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius-lg)",
    padding: 28,
    display: "flex",
    flexDirection: "column" as const,
    gap: 20,
    animation: "fade-in 250ms ease",
  },
  logo: {
    display: "flex",
    alignItems: "center",
    gap: 10,
    marginBottom: 4,
  },
  logoIcon: {
    fontSize: 26,
    color: "var(--accent)",
    lineHeight: 1,
  },
  logoText: {
    fontSize: 18,
    fontWeight: 700,
    letterSpacing: "-0.02em",
  },
  fieldset: {
    border: "1px solid var(--border)",
    borderRadius: "var(--radius)",
    padding: "10px 14px 14px",
    display: "flex",
    flexDirection: "column" as const,
    gap: 10,
  },
  legend: {
    padding: "0 6px",
    fontSize: 10,
    fontWeight: 600,
    textTransform: "uppercase" as const,
    letterSpacing: "0.08em",
    color: "var(--text-secondary)",
  },
  checkRow: {
    display: "flex",
    alignItems: "center",
    gap: 8,
    cursor: "pointer",
  },
  label: {
    display: "flex",
    flexDirection: "column" as const,
    gap: 5,
    fontSize: 11,
    fontWeight: 500,
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
    padding: "7px 0",
    borderRadius: "var(--radius-sm)",
    background: "none",
    color: "var(--text-secondary)",
    fontSize: 13,
    fontWeight: 500,
    transition: "background var(--transition), color var(--transition)",
  } as const,
  tabActive: {
    background: "var(--bg-hover)",
    color: "var(--text-primary)",
    fontWeight: 600,
  } as const,
  form: {
    display: "flex",
    flexDirection: "column" as const,
    gap: 12,
  },
  submitBtn: {
    marginTop: 6,
    padding: "10px 0",
    background: "var(--accent)",
    color: "#fff",
    borderRadius: "var(--radius-sm)",
    fontWeight: 700,
    fontSize: 14,
  } as const,
  divider: {
    display: "flex",
    alignItems: "center",
    gap: 10,
    marginTop: -6,
  },
  dividerLine: {
    flex: 1,
    height: 1,
    background: "var(--border)",
  },
  dividerLabel: {
    fontSize: 11,
    color: "var(--text-muted)",
  },
  guestBtn: {
    padding: "9px 0",
    background: "var(--bg-card)",
    color: "var(--text-secondary)",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius-sm)",
    fontWeight: 500,
    fontSize: 13,
    marginTop: -6,
  } as const,
} as const;
