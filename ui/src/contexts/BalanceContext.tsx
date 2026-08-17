import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import type { Balance } from "ts-sdk";
import { useAuth } from "./AuthContext.tsx";
import { useToast } from "./ToastContext.tsx";

// Single shared balance snapshot for the authenticated session. Several
// independent parts of the UI need the same live numbers — the header
// panel, the order form's balance check, and the faucet page — so they
// share one fetch instead of drifting out of sync with their own.
//
// This is a UX convenience only: it is not re-fetched on every keystroke
// or tick, so treat it as "recent", not "authoritative to the quantum".
// The server is still the real enforcement point for order placement.

interface BalanceContextValue {
  balances: Balance[];
  loading: boolean;
  /** Re-fetch from the server. Call after anything that moves the user's balance
   *  (an order fill, a faucet credit) so dependents pick up the new numbers. */
  refresh: () => Promise<void>;
}

const BalanceContext = createContext<BalanceContextValue | null>(null);

export function BalanceProvider({ children }: { children: ReactNode }) {
  const { session } = useAuth();
  const { showToast } = useToast();
  const [balances, setBalances] = useState<Balance[]>([]);
  const [loading, setLoading] = useState(false);

  const refresh = useCallback(async () => {
    if (!session) return;
    setLoading(true);
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
    if (!session) {
      setBalances([]);
      return;
    }
    void refresh();
  }, [session, refresh]);

  return (
    <BalanceContext.Provider value={{ balances, loading, refresh }}>
      {children}
    </BalanceContext.Provider>
  );
}

export function useBalances(): BalanceContextValue {
  const ctx = useContext(BalanceContext);
  if (!ctx) throw new Error("useBalances must be used inside BalanceProvider");
  return ctx;
}
