import {
  createContext,
  useCallback,
  useContext,
  useState,
  type ReactNode,
} from "react";
import { MatchingEngineClient, AuthenticatedClient } from "ts-sdk";

// ── Types ─────────────────────────────────────────────────────────────────

interface AuthState {
  client: MatchingEngineClient | null;
  session: AuthenticatedClient | null;
  username: string;
}

interface AuthContextValue extends AuthState {
  /** Create a public (unauthenticated) client so the trading view is accessible. */
  setClient: (client: MatchingEngineClient) => void;
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
  const [state, setState] = useState<AuthState>({
    client: null,
    session: null,
    username: "",
  });

  const setClient = useCallback((client: MatchingEngineClient) => {
    setState({ client, session: null, username: "" });
  }, []);

  const setSession = useCallback(
    (session: AuthenticatedClient, username: string) => {
      setState((prev) => ({ ...prev, session, username }));
    },
    [],
  );

  const logout = useCallback(() => {
    setState((prev) => ({ ...prev, session: null, username: "" }));
  }, []);

  const disconnect = useCallback(() => {
    setState({ client: null, session: null, username: "" });
  }, []);

  return (
    <AuthContext.Provider value={{ ...state, setClient, setSession, logout, disconnect }}>
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
