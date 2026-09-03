import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { AuthenticationError, MatchingEngineClient, AuthenticatedClient } from "ts-sdk";
import type { ApiTarget } from "../config.ts";
import { clearStoredAuth, loadStoredAuth, saveStoredAuth } from "../utils/authStorage.ts";
import { useToast } from "./ToastContext.tsx";

// ── Types ─────────────────────────────────────────────────────────────────

interface AuthState {
  client: MatchingEngineClient | null;
  session: AuthenticatedClient | null;
  username: string;
}

interface AuthContextValue extends AuthState {
  /** True while a persisted token is being checked against the API on first load. */
  restoring: boolean;
  /** Create a public (unauthenticated) client so the trading view is accessible. */
  setClient: (client: MatchingEngineClient, target: ApiTarget) => void;
  setSession: (session: AuthenticatedClient, username: string) => void;
  /** Drop the session but keep the client (reverts to guest view). */
  logout: () => void;
  /** Drop both client and session (back to connection form). */
  disconnect: () => void;
}

// ── Context ───────────────────────────────────────────────────────────────

const AuthContext = createContext<AuthContextValue | null>(null);

// ── Provider ──────────────────────────────────────────────────────────────

export function AuthProvider({ children }: { children: ReactNode }) {
  const { showToast } = useToast();
  const [state, setState] = useState<AuthState>({
    client: null,
    session: null,
    username: "",
  });
  const [restoring, setRestoring] = useState(() => loadStoredAuth() !== null);

  // Which server the current client points at. Kept in a ref rather than state
  // because setSession only ever reads it to persist alongside the token, and a
  // re-render on connect is already triggered by setClient.
  const target = useRef<ApiTarget | null>(null);

  useEffect(() => {
    const stored = loadStoredAuth();
    if (stored === null) return;

    let cancelled = false;

    void (async () => {
      try {
        const client = new MatchingEngineClient(stored.host, stored.port, {
          allowInsecure: stored.insecure,
        });
        const session = client.withToken(stored.token);

        // Doubles as the liveness check: a revoked, expired, or age-capped token
        // 401s here, and a good one comes back with its TTL pushed out, so an
        // active user never gets logged out mid-week.
        await session.refreshSession();
        if (cancelled) return;

        target.current = { host: stored.host, port: stored.port, insecure: stored.insecure };
        setState({ client, session, username: stored.username });
      } catch (err) {
        if (cancelled) return;
        // Only a rejected token means the stored session is worthless. A network
        // failure says nothing about the token, so it survives for the next load.
        if (err instanceof AuthenticationError) {
          clearStoredAuth();
          showToast("Your session expired — please sign in again", "info");
        } else {
          showToast(
            `Could not reach ${stored.host}:${stored.port} to restore your session`,
            "error",
          );
        }
      } finally {
        if (!cancelled) setRestoring(false);
      }
    })();

    return () => { cancelled = true; };
  }, [showToast]);

  const setClient = useCallback((client: MatchingEngineClient, apiTarget: ApiTarget) => {
    target.current = apiTarget;
    setState({ client, session: null, username: "" });
  }, []);

  const setSession = useCallback(
    (session: AuthenticatedClient, username: string) => {
      if (target.current !== null) {
        saveStoredAuth({ ...target.current, token: session.authToken, username });
      }
      setState((prev) => ({ ...prev, session, username }));
    },
    [],
  );

  const logout = useCallback(() => {
    clearStoredAuth();
    setState((prev) => ({ ...prev, session: null, username: "" }));
  }, []);

  const disconnect = useCallback(() => {
    clearStoredAuth();
    target.current = null;
    setState({ client: null, session: null, username: "" });
  }, []);

  return (
    <AuthContext.Provider
      value={{ ...state, restoring, setClient, setSession, logout, disconnect }}
    >
      {children}
    </AuthContext.Provider>
  );
}

// ── Hooks ─────────────────────────────────────────────────────────────────

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used inside AuthProvider");
  return ctx;
}

export function useSession(): {
  client: MatchingEngineClient;
  session: AuthenticatedClient;
  username: string;
} {
  const { client, session, username } = useAuth();
  if (!client || !session) {
    throw new Error("useSession requires an authenticated context");
  }
  return { client, session, username };
}
