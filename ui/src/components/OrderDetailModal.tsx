import { useEffect, useState } from "react";
import type { AuthenticatedClient, Order, OrderMatch } from "ts-sdk";
import { useToast } from "../contexts/ToastContext.tsx";
import { fmtBigInt, fmtDateTime, fmtUnits } from "../utils/format.ts";
import { SkeletonRows } from "./Skeleton.tsx";

interface Props {
  session: AuthenticatedClient;
  orderId: string;
  baseSymbol: string;
  quoteSymbol: string;
  baseDecimals: number;
  quoteDecimals: number;
  onClose: () => void;
}

// Weighted-by-fill-size average price, derived from the totals rather than averaging
// each match's own price — mirrors quoteAmount()'s price/quantity relationship
// (core/internal/orderbook/orderbook.go): quoteAmount = price * baseAmount / 10^baseDecimals,
// so summed over every fill, price = totalQuote * 10^baseDecimals / totalBase.
function avgFillPrice(matches: readonly OrderMatch[], baseDecimals: number): bigint | null {
  let totalBase = 0n;
  let totalQuote = 0n;
  for (const m of matches) {
    totalBase += m.baseAmount;
    totalQuote += m.quoteAmount;
  }
  if (totalBase === 0n) return null;
  return (totalQuote * 10n ** BigInt(baseDecimals)) / totalBase;
}

export function OrderDetailModal({
  session,
  orderId,
  baseSymbol,
  quoteSymbol,
  baseDecimals,
  quoteDecimals,
  onClose,
}: Props) {
  const { showToast } = useToast();
  const [order, setOrder] = useState<Order | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let active = true;
    setLoading(true);
    session
      .getOrder(orderId)
      .then((o) => {
        if (active) { setOrder(o); setLoading(false); }
      })
      .catch((err) => {
        if (!active) return;
        setLoading(false);
        showToast(
          `Failed to load order details: ${err instanceof Error ? err.message : String(err)}`,
          "error",
        );
        onClose();
      });
    return () => { active = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session, orderId]);

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const side = order?.side;
  const baseQty = order && side ? (side === "buy" ? order.wantQuantity : order.haveQuantity) : undefined;
  const quoteQty = order && side ? (side === "buy" ? order.haveQuantity : order.wantQuantity) : undefined;
  const matches = order?.matches ?? [];
  const avgPrice = matches.length > 0 ? avgFillPrice(matches, baseDecimals) : null;
  const totalFilledBase = matches.reduce((sum, m) => sum + m.baseAmount, 0n);

  return (
    <div style={s.backdrop} onClick={onClose}>
      <div style={s.modal} onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true">
        <div style={s.header}>
          <span style={s.title}>Order details</span>
          <button style={s.closeBtn} onClick={onClose}>✕</button>
        </div>

        {loading ? (
          <div style={{ padding: 16 }}><SkeletonRows count={6} /></div>
        ) : !order ? null : (
          <div style={s.body}>
            <div style={s.grid}>
              <Field label="Order ID" value={order.id} mono />
              {order.clientOrderId && <Field label="Client order ID" value={order.clientOrderId} mono />}
              <Field label="Type" value={order.type} />
              <Field label="Time in force" value={order.timeInForce} />
              <Field
                label="Side"
                value={side ?? "unknown"}
                color={side === "buy" ? "var(--green)" : side === "sell" ? "var(--red)" : undefined}
              />
              <Field label="Created" value={fmtDateTime(order.createdAt)} />
              {order.expiresAt !== undefined && <Field label="Expires" value={fmtDateTime(order.expiresAt)} />}

              {baseQty !== undefined && quoteQty !== undefined ? (
                <>
                  <Field label={`Amount (${baseSymbol})`} value={fmtUnits(baseQty, baseDecimals)} mono />
                  <Field label={`Notional (${quoteSymbol})`} value={fmtUnits(quoteQty, quoteDecimals)} mono />
                </>
              ) : (
                <Field
                  label="Amount"
                  value={`raw ${fmtBigInt(order.haveQuantity)} → ${fmtBigInt(order.wantQuantity)}`}
                  mono
                  title="Orientation unknown — the order's market was likely deleted"
                />
              )}

              {order.openOrder && side && (
                <>
                  <Field
                    label="Resting price"
                    value={`${fmtUnits(order.openOrder.price, quoteDecimals)} ${quoteSymbol}`}
                    mono
                  />
                  <Field
                    label={`Remaining (${baseSymbol})`}
                    value={fmtUnits(
                      side === "buy" ? order.openOrder.remainingWant : order.openOrder.remainingHave,
                      baseDecimals,
                    )}
                    mono
                  />
                  <Field
                    label={`Remaining (${quoteSymbol})`}
                    value={fmtUnits(
                      side === "buy" ? order.openOrder.remainingHave : order.openOrder.remainingWant,
                      quoteDecimals,
                    )}
                    mono
                  />
                </>
              )}

              {order.cancelledOrder && (
                <>
                  <Field label="Cancelled at" value={fmtDateTime(order.cancelledOrder.cancelledAt)} />
                  {side && (
                    <>
                      <Field
                        label={`Unfilled (${baseSymbol})`}
                        value={fmtUnits(
                          side === "buy" ? order.cancelledOrder.remainingWant : order.cancelledOrder.remainingHave,
                          baseDecimals,
                        )}
                        mono
                      />
                      <Field
                        label={`Unfilled (${quoteSymbol})`}
                        value={fmtUnits(
                          side === "buy" ? order.cancelledOrder.remainingHave : order.cancelledOrder.remainingWant,
                          quoteDecimals,
                        )}
                        mono
                      />
                    </>
                  )}
                </>
              )}
            </div>

            <div style={s.sectionTitle}>Fills{matches.length > 0 ? ` (${matches.length})` : ""}</div>
            {matches.length === 0 ? (
              <div style={s.emptyFills}>No fills yet.</div>
            ) : (
              <>
                {avgPrice !== null && (
                  <div style={s.summary}>
                    Avg price: <b>{fmtUnits(avgPrice, quoteDecimals)} {quoteSymbol}</b>
                    {"   ·   "}
                    Filled: <b>{fmtUnits(totalFilledBase, baseDecimals)} {baseSymbol}</b>
                  </div>
                )}
                <div style={s.matchTable}>
                  <div style={{ ...s.matchRow, ...s.matchHeadRow }}>
                    <span>Price</span>
                    <span>Amount</span>
                    <span>Role</span>
                    <span>Fee</span>
                    <span>Time</span>
                  </div>
                  {matches.map((m) => (
                    <div key={m.id} style={s.matchRow}>
                      <span>{fmtUnits(m.price, quoteDecimals)} {quoteSymbol}</span>
                      <span>{fmtUnits(m.baseAmount, baseDecimals)} {baseSymbol}</span>
                      <span>{m.isTaker ? "Taker" : "Maker"}</span>
                      <span>{fmtUnits(m.fee, side === "buy" ? baseDecimals : quoteDecimals)}</span>
                      <span>{fmtDateTime(m.matchTime)}</span>
                    </div>
                  ))}
                </div>
              </>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function Field({
  label, value, mono, color, title,
}: {
  label: string;
  value: string;
  mono?: boolean;
  color?: string;
  title?: string;
}) {
  return (
    <div style={s.field} title={title}>
      <span style={s.fieldLabel}>{label}</span>
      <span
        style={{
          ...s.fieldValue,
          ...(mono ? { fontFamily: "var(--font-mono)" } : {}),
          ...(color ? { color } : {}),
        }}
      >
        {value}
      </span>
    </div>
  );
}

// ── Styles ────────────────────────────────────────────────────────────────

const s = {
  backdrop: {
    position: "fixed" as const,
    inset: 0,
    background: "rgba(0,0,0,0.55)",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    zIndex: 1000,
    animation: "fade-in 150ms ease",
  },
  modal: {
    width: "min(640px, 92vw)",
    maxHeight: "85vh",
    overflowY: "auto" as const,
    background: "var(--bg-panel)",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius-lg)",
    boxShadow: "0 12px 40px rgba(0,0,0,0.5)",
  },
  header: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    padding: "14px 18px",
    borderBottom: "1px solid var(--border-subtle)",
    position: "sticky" as const,
    top: 0,
    background: "var(--bg-panel)",
  },
  title: {
    fontSize: 13,
    fontWeight: 700,
    color: "var(--text-primary)",
  },
  closeBtn: {
    background: "none",
    color: "var(--text-muted)",
    fontSize: 13,
    padding: "2px 6px",
  },
  body: {
    padding: 18,
    display: "flex",
    flexDirection: "column" as const,
    gap: 16,
  },
  grid: {
    display: "grid",
    gridTemplateColumns: "1fr 1fr",
    gap: 12,
  },
  field: {
    display: "flex",
    flexDirection: "column" as const,
    gap: 3,
    minWidth: 0,
  },
  fieldLabel: {
    fontSize: 10,
    fontWeight: 600,
    textTransform: "uppercase" as const,
    letterSpacing: "0.04em",
    color: "var(--text-muted)",
  },
  fieldValue: {
    fontSize: 13,
    color: "var(--text-primary)",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap" as const,
  },
  sectionTitle: {
    fontSize: 11,
    fontWeight: 700,
    textTransform: "uppercase" as const,
    letterSpacing: "0.04em",
    color: "var(--text-secondary)",
    borderTop: "1px solid var(--border-subtle)",
    paddingTop: 14,
  },
  emptyFills: {
    fontSize: 12,
    color: "var(--text-muted)",
  },
  summary: {
    fontSize: 12,
    color: "var(--text-secondary)",
  },
  matchTable: {
    border: "1px solid var(--border)",
    borderRadius: "var(--radius)",
    overflow: "hidden",
  },
  matchRow: {
    display: "grid",
    gridTemplateColumns: "1.3fr 1.1fr 0.7fr 0.9fr 1.2fr",
    gap: 8,
    padding: "7px 10px",
    fontSize: 11,
    fontFamily: "var(--font-mono)",
    borderBottom: "1px solid var(--border-subtle)",
  },
  matchHeadRow: {
    fontFamily: "var(--font-sans)",
    fontWeight: 600,
    fontSize: 10,
    textTransform: "uppercase" as const,
    letterSpacing: "0.04em",
    color: "var(--text-muted)",
    background: "var(--bg-card)",
  },
} as const;
