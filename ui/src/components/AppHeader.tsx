import type { ReactNode } from "react";
import { NavLink, Link } from "react-router-dom";
import { useAuth } from "../contexts/AuthContext.tsx";
import { useToast } from "../contexts/ToastContext.tsx";
import { BalancePanel } from "./BalancePanel.tsx";

interface Props {
  /** Page-specific content rendered after the nav links (e.g. the market selector). */
  leftExtra?: ReactNode;
}

const NAV_LINKS = [
  { to: "/", label: "Trade", end: true },
  { to: "/history", label: "History", end: false },
  { to: "/faucet", label: "Faucet", end: false },
] as const;

export function AppHeader({ leftExtra }: Props) {
  const { session, username, logout, disconnect } = useAuth();
  const { showToast } = useToast();

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
    <header style={s.header}>
      <div style={s.headerLeft}>
        <Link to="/" style={s.logo}>⬡ ME</Link>
        <nav style={s.nav}>
          {NAV_LINKS.map(({ to, label, end }) => (
            <NavLink
              key={to}
              to={to}
              end={end}
              style={({ isActive }) => ({
                ...s.navLink,
                ...(isActive ? s.navLinkActive : {}),
              })}
            >
              {label}
            </NavLink>
          ))}
        </nav>
        {leftExtra}
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
  );
}

// ── Styles ────────────────────────────────────────────────────────────────

const s = {
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
    textDecoration: "none",
  },
  nav: {
    display: "flex",
    alignItems: "center",
    gap: 4,
  },
  navLink: {
    padding: "5px 10px",
    borderRadius: "var(--radius-sm)",
    fontSize: 12,
    fontWeight: 500,
    color: "var(--text-secondary)",
    textDecoration: "none",
    transition: "background var(--transition), color var(--transition)",
  } as const,
  navLinkActive: {
    background: "var(--bg-hover)",
    color: "var(--text-primary)",
    fontWeight: 600,
  } as const,
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
} as const;
