import { useCallback, useEffect, useState } from "react";
import type {
  AuthenticatedClient,
  Instrument,
  Market,
  Operation,
  Order,
} from "ts-sdk";
import { useAuth } from "../contexts/AuthContext.tsx";
import { useToast } from "../contexts/ToastContext.tsx";
import { useInstruments } from "../hooks/useInstruments.ts";
import { AppHeader } from "../components/AppHeader.tsx";
import { MarketSelector } from "../components/MarketSelector.tsx";
import { SignInRequired } from "../components/SignInRequired.tsx";
import { SkeletonRows } from "../components/Skeleton.tsx";
import { fmtBigInt, fmtDateTime, fmtUnits, shortId } from "../utils/format.ts";

type Tab = "orders" | "operations";

// ── Orders tab ───────────────────────────────────────────────────────────
//
// NOTE: the API's order response never tells us which instrument was "have"
// vs "want" for a filled or cancelled order (only open orders carry `side`,
// via open_orders.side — see api/internal/orders/handler.go). So price and
// a base/quote-scaled amount can only be shown for orders still resting in
// the book; filled/cancelled rows fall back to raw have/want quantities.
// Flagging this as a real API gap rather than guessing at an orientation.

function orderStatus(order: Order): { label: string; color: string } {
  if (order.cancelledOrder) return { label: "Cancelled", color: "var(--red)" };
  if (order.openOrder) {
    const partial = order.openOrder.remainingHave < order.haveQuantity;
    return partial
      ? { label: "Partially filled", color: "var(--accent)" }
      : { label: "Open", color: "var(--text-secondary)" };
  }
  return { label: "Filled", color: "var(--green)" };
}

function OrdersTab({
  session,
  instruments,
}: {
  session: AuthenticatedClient;
  instruments: Instrument[];
}) {
  const { showToast } = useToast();
  const [market, setMarket] = useState("");
  const [selectedMarket, setSelectedMarket] = useState<Market | null>(null);
  const [orders, setOrders] = useState<Order[]>([]);
  const [loading, setLoading] = useState(false);

  const baseSymbol = selectedMarket?.baseSymbol ?? "";
  const quoteSymbol = selectedMarket?.quoteSymbol ?? "";
  const baseDecimals = instruments.find((i) => i.symbol === baseSymbol)?.decimals ?? 0;
  const quoteDecimals = instruments.find((i) => i.symbol === quoteSymbol)?.decimals ?? 0;

  const fetchOrders = useCallback(async () => {
    if (!market) { setOrders([]); return; }
    setLoading(true);
    try {
      // showOpen + showCancelled pull in the open_orders / cancelled_orders
      // sub-objects we need to tell open/partial/cancelled apart — without
      // them every row looks indistinguishable from "filled".
      const result = await session.getOrders({
        market,
        showOpen: true,
        showCancelled: true,
        limit: 100,
      });
      setOrders(result);
    } catch (err) {
      showToast(
        `Failed to load order history: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
    } finally {
      setLoading(false);
    }
  }, [session, market, showToast]);

  useEffect(() => { void fetchOrders(); }, [fetchOrders]);

  return (
    <div style={s.tabBody}>
      <div style={s.toolbar}>
        <MarketSelector
          value={market}
          onChange={(ref: string, m: Market) => { setMarket(ref); setSelectedMarket(m); }}
        />
        <button
          onClick={() => void fetchOrders()}
          disabled={loading || !market}
          style={s.refreshBtn}
        >
          {loading ? "…" : "↻ Refresh"}
        </button>
      </div>

      {!market ? (
        <div style={s.empty}>Pick a market to view its order history.</div>
      ) : loading && orders.length === 0 ? (
        <div style={{ padding: 14 }}><SkeletonRows count={6} /></div>
      ) : orders.length === 0 ? (
        <div style={s.empty}>No orders for this market yet.</div>
      ) : (
        <div style={s.table}>
          <div style={{ ...s.row, ...s.headRow }}>
            <span>Status</span>
            <span>Side</span>
            <span>Price</span>
            <span>Base amount</span>
            <span>Quote amount</span>
            <span>Created</span>
            <span>Order ID</span>
          </div>
          {orders.map((order) => {
            const status = orderStatus(order);
            const side = order.openOrder?.side;

            // Split have/want into base/quote legs (see orderLegDecimals) — only
            // possible when side is known, i.e. the order is still open. For a
            // filled/cancelled order we genuinely don't know the orientation
            // (a real API gap, not a UI shortcut — see the note at the top of
            // this file), so both columns fall back to "—" per ui/CLAUDE.md
            // rule 5 rather than guessing at a scale.
            let baseAmount = "—";
            let quoteAmount = "—";
            let rawTitle: string | undefined;
            if (order.openOrder && side) {
              const oo = order.openOrder;
              const baseRemaining = side === "buy" ? oo.remainingWant : oo.remainingHave;
              const baseOriginal = side === "buy" ? order.wantQuantity : order.haveQuantity;
              const quoteRemaining = side === "buy" ? oo.remainingHave : oo.remainingWant;
              const quoteOriginal = side === "buy" ? order.haveQuantity : order.wantQuantity;
              baseAmount = `${fmtUnits(baseRemaining, baseDecimals)} / ${fmtUnits(baseOriginal, baseDecimals)}`;
              quoteAmount = `${fmtUnits(quoteRemaining, quoteDecimals)} / ${fmtUnits(quoteOriginal, quoteDecimals)}`;
            } else {
              rawTitle = `Orientation unknown — raw have→want: ${fmtBigInt(order.haveQuantity)} → ${fmtBigInt(order.wantQuantity)}`;
            }

            return (
              <div key={order.id} style={s.row} title={rawTitle}>
                <span style={{ color: status.color, fontWeight: 600 }}>{status.label}</span>
                <span style={{
                  color: side === "buy" ? "var(--green)" : side === "sell" ? "var(--red)" : "var(--text-muted)",
                  fontWeight: 600,
                  textTransform: "uppercase" as const,
                }}>
                  {side ?? "—"}
                </span>
                <span style={{ fontFamily: "var(--font-mono)" }}>
                  {order.openOrder ? `${fmtUnits(order.openOrder.price, quoteDecimals)} ${quoteSymbol}` : "—"}
                </span>
                <span style={{ fontFamily: "var(--font-mono)" }}>
                  {baseAmount !== "—" ? `${baseAmount} ${baseSymbol}` : baseAmount}
                </span>
                <span style={{ fontFamily: "var(--font-mono)" }}>
                  {quoteAmount !== "—" ? `${quoteAmount} ${quoteSymbol}` : quoteAmount}
                </span>
                <span style={{ color: "var(--text-muted)" }}>{fmtDateTime(order.createdAt)}</span>
                <span style={{ color: "var(--text-muted)", fontFamily: "var(--font-mono)" }}>
                  {shortId(order.id)}
                </span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

// ── Operations tab ───────────────────────────────────────────────────────

function OperationsTab({
  session,
  instruments,
}: {
  session: AuthenticatedClient;
  instruments: Instrument[];
}) {
  const { showToast } = useToast();
  const [operations, setOperations] = useState<Operation[]>([]);
  const [loading, setLoading] = useState(false);

  const fetchOperations = useCallback(async () => {
    setLoading(true);
    try {
      const result = await session.getOperations({ limit: 100 });
      setOperations(result);
    } catch (err) {
      showToast(
        `Failed to load operations: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
    } finally {
      setLoading(false);
    }
  }, [session, showToast]);

  useEffect(() => { void fetchOperations(); }, [fetchOperations]);

  const SIGN: Record<string, "+" | "−"> = {
    deposit: "+",
    unfreeze: "+",
    withdraw: "−",
    freeze: "−",
  };
  const COLOR: Record<string, string> = {
    deposit: "var(--green)",
    unfreeze: "var(--green)",
    withdraw: "var(--red)",
    freeze: "var(--red)",
  };

  return (
    <div style={s.tabBody}>
      <div style={s.toolbar}>
        <span style={{ color: "var(--text-muted)", fontSize: 12 }}>
          Deposits, withdrawals, and freezes applied to your account (admin-driven — faucet
          credits show up here too, reason: "faucet").
        </span>
        <button
          onClick={() => void fetchOperations()}
          disabled={loading}
          style={s.refreshBtn}
        >
          {loading ? "…" : "↻ Refresh"}
        </button>
      </div>

      {loading && operations.length === 0 ? (
        <div style={{ padding: 14 }}><SkeletonRows count={6} /></div>
      ) : operations.length === 0 ? (
        <div style={s.empty}>No operations yet.</div>
      ) : (
        <div style={s.table}>
          <div style={{ ...s.opRow, ...s.headRow }}>
            <span>Type</span>
            <span>Amount</span>
            <span>Reason</span>
            <span>Date</span>
          </div>
          {operations.map((op) => {
            const decimals = instruments.find((i) => i.symbol === op.symbol)?.decimals ?? 0;
            return (
              <div key={op.id} style={s.opRow}>
                <span style={{ color: COLOR[op.type], fontWeight: 600, textTransform: "capitalize" as const }}>
                  {op.type}
                </span>
                <span style={{ fontFamily: "var(--font-mono)", color: COLOR[op.type] }}>
                  {SIGN[op.type]}{fmtUnits(op.amount, decimals)} {op.symbol}
                </span>
                <span style={{ color: "var(--text-muted)" }}>{op.reason ?? "—"}</span>
                <span style={{ color: "var(--text-muted)" }}>{fmtDateTime(op.createdAt)}</span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

// ── Page ─────────────────────────────────────────────────────────────────

export function HistoryPage() {
  const { client, session } = useAuth();
  const instruments = useInstruments(client);
  const [tab, setTab] = useState<Tab>("orders");

  return (
    <div style={s.shell}>
      <AppHeader />
      <div style={s.content}>
        {!session ? (
          <SignInRequired what="your order and operation history" />
        ) : (
          <>
            <div style={s.tabs}>
              {(["orders", "operations"] as Tab[]).map((t) => (
                <button
                  key={t}
                  onClick={() => setTab(t)}
                  style={{ ...s.tab, ...(tab === t ? s.tabActive : {}) }}
                >
                  {t === "orders" ? "Order History" : "Operations"}
                </button>
              ))}
            </div>
            {tab === "orders"
              ? <OrdersTab session={session} instruments={instruments} />
              : <OperationsTab session={session} instruments={instruments} />}
          </>
        )}
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
    overflow: "hidden",
    padding: "16px 24px",
    maxWidth: 1100,
    width: "100%",
    margin: "0 auto",
    boxSizing: "border-box" as const,
  },
  tabs: {
    display: "flex",
    gap: 6,
    marginBottom: 14,
    flexShrink: 0,
  },
  tab: {
    padding: "7px 16px",
    borderRadius: "var(--radius-sm)",
    background: "var(--bg-card)",
    color: "var(--text-secondary)",
    fontSize: 12,
    fontWeight: 600,
    border: "1px solid var(--border)",
  } as const,
  tabActive: {
    background: "var(--bg-hover)",
    color: "var(--text-primary)",
    borderColor: "var(--accent)",
  } as const,
  tabBody: {
    display: "flex",
    flexDirection: "column" as const,
    overflow: "hidden",
    minHeight: 0,
    animation: "fade-in 200ms ease",
  },
  toolbar: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
    marginBottom: 10,
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
  empty: {
    padding: 30,
    textAlign: "center" as const,
    color: "var(--text-muted)",
    fontSize: 13,
  },
  table: {
    overflowY: "auto" as const,
    border: "1px solid var(--border)",
    borderRadius: "var(--radius)",
    background: "var(--bg-panel)",
  },
  row: {
    display: "grid",
    gridTemplateColumns: "1fr 0.6fr 1.1fr 1.6fr 1.6fr 1.3fr 0.9fr",
    gap: 10,
    padding: "9px 14px",
    fontSize: 12,
    borderBottom: "1px solid var(--border-subtle)",
    alignItems: "center",
  },
  opRow: {
    display: "grid",
    gridTemplateColumns: "1fr 1.6fr 1.6fr 1.4fr",
    gap: 10,
    padding: "9px 14px",
    fontSize: 12,
    borderBottom: "1px solid var(--border-subtle)",
    alignItems: "center",
  },
  headRow: {
    color: "var(--text-muted)",
    fontSize: 10,
    fontWeight: 600,
    letterSpacing: "0.04em",
    textTransform: "uppercase" as const,
    background: "var(--bg-card)",
  },
} as const;
