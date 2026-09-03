import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  AuthenticationError,
  SessionOrigin,
  SessionScope,
  type AuthenticatedClient,
  type CreateTokenResult,
  type Session,
} from "ts-sdk";
import { useAuth } from "../contexts/AuthContext.tsx";
import { useToast } from "../contexts/ToastContext.tsx";
import { AppHeader } from "../components/AppHeader.tsx";
import { SignInRequired } from "../components/SignInRequired.tsx";
import { SkeletonRows } from "../components/Skeleton.tsx";
import { fmtDateTime, fmtRelative, shortId } from "../utils/format.ts";
import { sessionIdForToken } from "../utils/token.ts";

const ORIGIN_LABEL: Record<string, string> = {
  [SessionOrigin.Login]: "Sign-in",
  [SessionOrigin.Minted]: "API token",
};

function errMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

// ── Minting panel ────────────────────────────────────────────────────────
//
// The raw token comes back exactly once — the server only ever stores its
// hash — so it stays on screen until dismissed rather than being cleared by
// the next render or a list refresh.

function MintPanel({ session, onMinted }: { session: AuthenticatedClient; onMinted: () => void }) {
  const { showToast } = useToast();
  const [scope, setScope] = useState<SessionScope>(SessionScope.Read);
  const [minting, setMinting] = useState(false);
  const [minted, setMinted] = useState<CreateTokenResult | null>(null);

  async function mint() {
    setMinting(true);
    try {
      const result = await session.createToken(scope);
      setMinted(result);
      showToast(`Minted a ${result.scope}-scoped token`, "success");
      onMinted();
    } catch (err) {
      showToast(errMessage(err), "error");
    } finally {
      setMinting(false);
    }
  }

  return (
    <div style={s.card}>
      <div style={s.cardHead}>
        <span style={s.cardTitle}>Create API token</span>
        <span style={s.cardSubtitle}>
          A standalone bearer token for a bot or script — it appears below as its own session and
          can be revoked without touching your sign-in.
        </span>
      </div>

      <div style={s.mintRow}>
        <label style={s.label}>
          Scope
          <select
            value={scope}
            onChange={(e) => setScope(e.target.value as SessionScope)}
            disabled={minting}
          >
            <option value={SessionScope.Read}>read — view only, cannot trade</option>
            <option value={SessionScope.Write}>write — can place and cancel orders</option>
          </select>
        </label>
        <button onClick={() => void mint()} disabled={minting} style={s.primaryBtn}>
          {minting ? "Creating…" : "Create token"}
        </button>
      </div>

      {minted && (
        <div style={s.tokenBox}>
          <div style={s.tokenWarn}>
            Copy this now — it is shown only once and cannot be retrieved again.
          </div>
          <code style={s.tokenValue}>{minted.token}</code>
          <div style={s.tokenMeta}>
            <span>scope {minted.scope}</span>
            <span>expires {fmtDateTime(minted.expiresAt)} ({fmtRelative(minted.expiresAt)})</span>
            <button onClick={() => setMinted(null)} style={s.linkBtn}>Dismiss</button>
          </div>
        </div>
      )}
    </div>
  );
}

// ── Session list ─────────────────────────────────────────────────────────

function SessionRow({
  session,
  isCurrent,
  revoking,
  onRevoke,
}: {
  session: Session;
  isCurrent: boolean;
  revoking: boolean;
  onRevoke: () => void;
}) {
  return (
    <div style={{ ...s.row, ...(isCurrent ? s.rowCurrent : {}) }}>
      <div style={s.rowMain}>
        <div style={s.badges}>
          {isCurrent && <span style={{ ...s.badge, ...s.badgeCurrent }}>This device</span>}
          <span style={s.badge}>{ORIGIN_LABEL[session.origin] ?? session.origin}</span>
          <span
            style={{
              ...s.badge,
              ...(session.scope === SessionScope.Write ? s.badgeWrite : s.badgeRead),
            }}
          >
            {session.scope}
          </span>
        </div>
        <div style={s.userAgent} title={session.userAgent ?? "unknown client"}>
          {session.userAgent ?? "unknown client"}
        </div>
        <div style={s.meta}>
          <span>{session.ipAddress ?? "no ip recorded"}</span>
          <span>·</span>
          <span title={fmtDateTime(session.createdAt)}>
            started {fmtRelative(session.createdAt)}
          </span>
          <span>·</span>
          <span title={fmtDateTime(session.expiresAt)}>
            expires {fmtRelative(session.expiresAt)}
          </span>
          <span>·</span>
          <span style={s.sessionId} title={session.sessionId}>
            {shortId(session.sessionId)}
          </span>
        </div>
      </div>
      <button
        onClick={onRevoke}
        disabled={revoking}
        style={{ ...s.revokeBtn, ...(isCurrent ? s.revokeBtnCurrent : {}) }}
      >
        {revoking ? "…" : isCurrent ? "Revoke & sign out" : "Revoke"}
      </button>
    </div>
  );
}

function SessionsView({ session }: { session: AuthenticatedClient }) {
  const { logout } = useAuth();
  const { showToast } = useToast();
  const navigate = useNavigate();

  const [sessions, setSessions] = useState<Session[]>([]);
  const [loading, setLoading] = useState(true);
  const [revokingId, setRevokingId] = useState<string | null>(null);
  const [currentId, setCurrentId] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    void sessionIdForToken(session.authToken).then((id) => {
      if (!cancelled) setCurrentId(id);
    });
    return () => { cancelled = true; };
  }, [session]);

  // Kept separate from refresh() so callers that need to see a dead token can:
  // revoking our own session still returns 200 (the auth middleware ran before
  // the revocation), so the 401 only ever shows up on the request after it.
  const loadSessions = useCallback(async () => {
    const list = await session.getActiveSessions();
    setSessions([...list].sort((a, b) => b.createdAt - a.createdAt));
  }, [session]);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      await loadSessions();
    } catch (err) {
      showToast(`Failed to load sessions: ${errMessage(err)}`, "error");
    } finally {
      setLoading(false);
    }
  }, [loadSessions, showToast]);

  useEffect(() => { void refresh(); }, [refresh]);

  const signOutHere = useCallback((message: string) => {
    logout();
    showToast(message, "info");
    void navigate("/");
  }, [logout, showToast, navigate]);

  async function revoke(target: Session) {
    setRevokingId(target.sessionId);
    try {
      await session.revokeSession(target.sessionId);
      if (target.sessionId === currentId) {
        signOutHere("Signed out on this device");
        return;
      }
      // Without a secure context there is no "this device" marker to compare
      // against, so the row just revoked may still have been our own — this
      // reload is where that comes out.
      await loadSessions();
      showToast("Session revoked", "success");
    } catch (err) {
      if (err instanceof AuthenticationError && currentId === null) {
        signOutHere("That was this device's session — signed out");
        return;
      }
      showToast(`Failed to revoke session: ${errMessage(err)}`, "error");
    } finally {
      setRevokingId(null);
    }
  }

  return (
    <>
      <MintPanel session={session} onMinted={() => void refresh()} />

      <div style={s.card}>
        <div style={s.toolbar}>
          <div style={s.cardHead}>
            <span style={s.cardTitle}>Active sessions</span>
            <span style={s.cardSubtitle}>
              Every non-expired, non-revoked token issued for your account.
            </span>
          </div>
          <button onClick={() => void refresh()} disabled={loading} style={s.refreshBtn}>
            {loading ? "…" : "↻ Refresh"}
          </button>
        </div>

        {loading && sessions.length === 0 ? (
          <SkeletonRows count={3} />
        ) : sessions.length === 0 ? (
          <div style={s.empty}>No active sessions.</div>
        ) : (
          <div style={s.list}>
            {sessions.map((row) => (
              <SessionRow
                key={row.sessionId}
                session={row}
                isCurrent={row.sessionId === currentId}
                revoking={revokingId === row.sessionId}
                onRevoke={() => void revoke(row)}
              />
            ))}
          </div>
        )}
      </div>
    </>
  );
}

export function SessionsPage() {
  const { session } = useAuth();

  return (
    <div style={s.shell}>
      <AppHeader />
      <div style={s.content}>
        {!session ? <SignInRequired what="your sessions" /> : <SessionsView session={session} />}
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
    flexDirection: "column" as const,
    gap: 16,
    overflowY: "auto" as const,
    padding: "16px 24px",
    maxWidth: 860,
    width: "100%",
    margin: "0 auto",
    boxSizing: "border-box" as const,
  },
  card: {
    background: "var(--bg-panel)",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius-lg)",
    padding: 18,
    display: "flex",
    flexDirection: "column" as const,
    gap: 14,
    flexShrink: 0,
    animation: "fade-in 200ms ease",
  },
  cardHead: {
    display: "flex",
    flexDirection: "column" as const,
    gap: 3,
  },
  cardTitle: {
    fontSize: 14,
    fontWeight: 700,
  },
  cardSubtitle: {
    fontSize: 11,
    color: "var(--text-muted)",
  },
  toolbar: {
    display: "flex",
    alignItems: "flex-start",
    justifyContent: "space-between",
    gap: 12,
  },
  mintRow: {
    display: "flex",
    alignItems: "flex-end",
    gap: 10,
  },
  label: {
    flex: 1,
    display: "flex",
    flexDirection: "column" as const,
    gap: 5,
    fontSize: 11,
    fontWeight: 500,
    color: "var(--text-secondary)",
  },
  primaryBtn: {
    padding: "8px 16px",
    background: "var(--accent)",
    color: "#fff",
    borderRadius: "var(--radius-sm)",
    fontWeight: 600,
    fontSize: 13,
    flexShrink: 0,
  },
  refreshBtn: {
    background: "var(--bg-hover)",
    color: "var(--text-secondary)",
    padding: "6px 12px",
    borderRadius: "var(--radius-sm)",
    fontSize: 12,
    fontWeight: 500,
    flexShrink: 0,
  },
  tokenBox: {
    display: "flex",
    flexDirection: "column" as const,
    gap: 8,
    padding: 12,
    background: "var(--accent-dim)",
    border: "1px solid var(--accent)",
    borderRadius: "var(--radius)",
  },
  tokenWarn: {
    fontSize: 11,
    fontWeight: 600,
    color: "var(--accent-hover)",
  },
  tokenValue: {
    fontFamily: "var(--font-mono)",
    fontSize: 12,
    wordBreak: "break-all" as const,
    userSelect: "all" as const,
    background: "var(--bg-base)",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius-sm)",
    padding: "8px 10px",
  },
  tokenMeta: {
    display: "flex",
    alignItems: "center",
    gap: 12,
    fontSize: 11,
    color: "var(--text-secondary)",
  },
  linkBtn: {
    marginLeft: "auto",
    background: "none",
    color: "var(--text-secondary)",
    fontSize: 11,
    textDecoration: "underline",
    padding: 0,
  },
  list: {
    display: "flex",
    flexDirection: "column" as const,
    border: "1px solid var(--border)",
    borderRadius: "var(--radius)",
    overflow: "hidden",
  },
  row: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
    padding: "11px 14px",
    borderBottom: "1px solid var(--border-subtle)",
    minWidth: 0,
  },
  rowCurrent: {
    background: "var(--bg-card)",
  },
  rowMain: {
    display: "flex",
    flexDirection: "column" as const,
    gap: 5,
    minWidth: 0,
  },
  badges: {
    display: "flex",
    alignItems: "center",
    gap: 6,
  },
  badge: {
    fontSize: 10,
    fontWeight: 600,
    textTransform: "uppercase" as const,
    letterSpacing: "0.04em",
    color: "var(--text-secondary)",
    background: "var(--bg-hover)",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius-sm)",
    padding: "2px 7px",
  },
  badgeCurrent: {
    color: "var(--accent-hover)",
    background: "var(--accent-dim)",
    borderColor: "var(--accent)",
  },
  badgeRead: {
    color: "var(--text-secondary)",
  },
  badgeWrite: {
    color: "var(--green)",
    background: "var(--green-dim)",
    borderColor: "var(--green)",
  },
  userAgent: {
    fontSize: 12,
    color: "var(--text-primary)",
    whiteSpace: "nowrap" as const,
    overflow: "hidden",
    textOverflow: "ellipsis",
  },
  meta: {
    display: "flex",
    flexWrap: "wrap" as const,
    alignItems: "center",
    gap: 6,
    fontSize: 11,
    color: "var(--text-muted)",
  },
  sessionId: {
    fontFamily: "var(--font-mono)",
  },
  revokeBtn: {
    background: "var(--bg-hover)",
    color: "var(--text-secondary)",
    padding: "6px 12px",
    borderRadius: "var(--radius-sm)",
    fontSize: 12,
    fontWeight: 500,
    flexShrink: 0,
  },
  revokeBtnCurrent: {
    color: "var(--red)",
    background: "var(--red-dim)",
  },
  empty: {
    padding: 24,
    textAlign: "center" as const,
    color: "var(--text-muted)",
    fontSize: 13,
  },
} as const;
