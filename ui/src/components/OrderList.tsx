import { useCallback, useEffect, useState } from "react";
import type { Order } from "ts-sdk";
import { useSession } from "../contexts/AuthContext.tsx";
import { useToast } from "../contexts/ToastContext.tsx";
import { fmtDateTime, shortId } from "../utils/format.ts";
import { Skeleton, SkeletonRows } from "./Skeleton.tsx";

interface Props {
  market: string;
  refreshSignal?: number; // increment to force a refresh
}

export function OrderList({ market, refreshSignal }: Props) {
  const { session } = useSession();
  const { showToast } = useToast();
  const [orders, setOrders] = useState<Order[]>([]);
  const [loading, setLoading] = useState(false);
  const [cancelling, setCancelling] = useState<Set<string>>(new Set());

  const fetchOrders = useCallback(async () => {
    setLoading(true);
    try {
      const result = await session.getOrders({
        market: market || undefined,
        showOpen: true,
        limit: 50,
      });
      setOrders(result);
    } catch (err) {
      showToast(
        `Failed to load orders: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
    } finally {
      setLoading(false);
    }
  }, [session, market, showToast]);

  useEffect(() => {
    void fetchOrders();
  }, [fetchOrders, refreshSignal]);

  async function cancelOrder(orderId: string) {
    setCancelling((prev) => new Set([...prev, orderId]));
    try {
      const { results } = await session.cancelOrders([orderId]);
      const result = results[0];
      if (result?.error) {
        showToast(`Cancel failed: ${result.error}`, "error");
      } else {
        showToast("Cancel request sent", "info");
        // Remove optimistically.
        setOrders((prev) => prev.filter((o) => o.id !== orderId));
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setCancelling((prev) => {
        const next = new Set(prev);
        next.delete(orderId);
        return next;
      });
    }
  }

  const openOrders = orders.filter((o) => o.openOrder != null);

  return (
    <div style={s.container}>
      <div style={s.header}>
        <span style={s.title}>Open Orders</span>
        <button onClick={() => void fetchOrders()} style={s.refreshBtn} disabled={loading}>
          {loading ? "…" : "↻"}
        </button>
      </div>

      {loading && openOrders.length === 0 ? (
        <div style={{ padding: 10 }}>
          <SkeletonRows count={3} />
        </div>
      ) : openOrders.length === 0 ? (
        <div style={s.empty}>No open orders</div>
      ) : (
        <div style={s.list}>
          {openOrders.map((order) => {
            const o = order.openOrder!;
            const isBuy = o.side === "buy";
            return (
              <div key={order.id} style={s.row}>
                <div style={s.rowTop}>
                  <span
                    style={{ color: isBuy ? "var(--green)" : "var(--red)", fontWeight: 600 }}
                  >
                    {o.side.toUpperCase()}
                  </span>
                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}>
                    {o.price.toLocaleString()}
                  </span>
                  <button
                    onClick={() => void cancelOrder(order.id)}
                    disabled={cancelling.has(order.id)}
                    style={s.cancelBtn}
                  >
                    {cancelling.has(order.id) ? "…" : "Cancel"}
                  </button>
                </div>
                <div style={s.rowSub}>
                  <span style={{ color: "var(--text-muted)" }}>
                    id: {shortId(order.id)}
                  </span>
                  <span style={{ color: "var(--text-muted)" }}>
                    rem: {o.remainingHave.toLocaleString()}
                  </span>
                  <span style={{ color: "var(--text-muted)" }}>
                    {fmtDateTime(order.createdAt)}
                  </span>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

// ── Styles ────────────────────────────────────────────────────────────────

const s = {
  container: {
    display: "flex",
    flexDirection: "column" as const,
    borderTop: "1px solid var(--border)",
    animation: "fade-in 200ms ease",
  },
  header: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    padding: "10px 14px 6px",
    borderBottom: "1px solid var(--border-subtle)",
  },
  title: {
    fontSize: 11,
    fontWeight: 600,
    textTransform: "uppercase" as const,
    letterSpacing: "0.06em",
    color: "var(--text-secondary)",
  },
  refreshBtn: {
    background: "none",
    color: "var(--text-secondary)",
    fontSize: 16,
    padding: "0 4px",
  },
  empty: {
    padding: "16px 14px",
    color: "var(--text-muted)",
    fontSize: 12,
    textAlign: "center" as const,
  },
  list: {
    overflowY: "auto" as const,
    maxHeight: 280,
  },
  row: {
    padding: "8px 14px",
    borderBottom: "1px solid var(--border-subtle)",
    display: "flex",
    flexDirection: "column" as const,
    gap: 3,
  },
  rowTop: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 8,
    fontSize: 12,
  },
  rowSub: {
    display: "flex",
    gap: 12,
    fontSize: 10,
  },
  cancelBtn: {
    background: "var(--red-dim)",
    color: "var(--red)",
    padding: "2px 8px",
    borderRadius: "var(--radius-sm)",
    fontSize: 11,
    fontWeight: 600,
  },
} as const;
