import { useState } from "react";
import { OrderSide, OrderType, TimeInForce } from "ts-sdk";
import { useSession } from "../contexts/AuthContext.tsx";
import { useToast } from "../contexts/ToastContext.tsx";
import { useBalances } from "../contexts/BalanceContext.tsx";
import { fmtUnits, parseUnits } from "../utils/format.ts";

interface Props {
  market: string;
  baseSymbol: string;
  quoteSymbol: string;
  baseDecimals: number;
  quoteDecimals: number;
  onOrderPlaced?: () => void;
}

export function OrderForm({
  market,
  baseSymbol,
  quoteSymbol,
  baseDecimals,
  quoteDecimals,
  onOrderPlaced,
}: Props) {
  const { session } = useSession();
  const { showToast } = useToast();
  const { balances, refresh: refreshBalances } = useBalances();

  const [side, setSide] = useState<"buy" | "sell">("buy");
  const [price, setPrice] = useState("");
  const [quantity, setQuantity] = useState("");
  const [tif, setTif] = useState<string>(TimeInForce.GoodTillCancel);
  const [loading, setLoading] = useState(false);

  const isBuy = side === "buy";

  // A buy spends quote asset (price × qty); a sell spends base asset (qty).
  const availableSymbol = isBuy ? quoteSymbol : baseSymbol;
  const availableDecimals = isBuy ? quoteDecimals : baseDecimals;
  const available = balances.find((b) => b.symbol === availableSymbol)?.balance ?? 0n;

  const priceBig = parseUnits(price, quoteDecimals);
  const qtyBig = parseUnits(quantity, baseDecimals);
  // priceBig is quote-quanta per whole base coin; qtyBig is base-quanta.
  // Multiplying them raw is not itself a quote-quanta value — it has to be
  // normalized by the base scale first, same as core's quoteAmount() (see
  // ui/CLAUDE.md rule 4). Skipping this produced the "Needs 64,500,000,000
  // USDT" bug: priceBig * qtyBig without the division.
  const needed =
    priceBig !== undefined && qtyBig !== undefined
      ? isBuy ? (priceBig * qtyBig) / (10n ** BigInt(baseDecimals)) : qtyBig
      : undefined;
  // This is a client-side UX guard, not the source of truth — the server
  // still enforces the real balance check when the order is submitted.
  const insufficientBalance = needed !== undefined && needed > available;

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!market) { showToast("Select a market first", "error"); return; }

    if (priceBig === undefined) {
      showToast(`Invalid price — enter a decimal with up to ${quoteDecimals} places`, "error");
      return;
    }
    if (qtyBig === undefined || qtyBig === 0n) {
      showToast(`Invalid quantity — enter a decimal with up to ${baseDecimals} places`, "error");
      return;
    }
    if (needed !== undefined && needed > available) {
      showToast(
        `Insufficient ${availableSymbol} balance — need ${fmtUnits(needed, availableDecimals)}, have ${fmtUnits(available, availableDecimals)}`,
        "error",
      );
      return;
    }

    setLoading(true);
    try {
      const { results } = await session.createOrders([
        {
          market,
          side: side === "buy" ? OrderSide.Buy : OrderSide.Sell,
          type: OrderType.Limit,
          timeInForce: tif as typeof TimeInForce[keyof typeof TimeInForce],
          price: priceBig,
          quantity: qtyBig,
        },
      ]);
      const result = results[0];
      if (result?.error) {
        showToast(`Order rejected: ${result.error}`, "error");
      } else {
        showToast(`Order placed — id: ${result?.orderId?.slice(0, 8) ?? "?"}…`, "success");
        setPrice("");
        setQuantity("");
        onOrderPlaced?.();
        void refreshBalances();
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <form onSubmit={submit} style={s.form}>
      <div style={s.header}>
        <span style={s.title}>Place Order</span>
      </div>

      {/* Side toggle */}
      <div style={s.sideRow}>
        <button
          type="button"
          onClick={() => setSide("buy")}
          style={{ ...s.sideBtn, ...(isBuy ? s.buyActive : s.sideInactive) }}
        >
          Buy
        </button>
        <button
          type="button"
          onClick={() => setSide("sell")}
          style={{ ...s.sideBtn, ...(!isBuy ? s.sellActive : s.sideInactive) }}
        >
          Sell
        </button>
      </div>

      {/* Fields */}
      <label style={s.label}>
        Price {quoteSymbol && `(${quoteSymbol})`}
        <input
          value={price}
          onChange={(e) => setPrice(e.target.value)}
          placeholder={quoteDecimals > 0 ? `e.g. 63448.${"0".repeat(Math.min(quoteDecimals, 2))}` : "e.g. 63448"}
          autoComplete="off"
          inputMode="decimal"
        />
      </label>

      <label style={s.label}>
        Quantity {baseSymbol && `(${baseSymbol})`}
        <input
          value={quantity}
          onChange={(e) => setQuantity(e.target.value)}
          placeholder={baseDecimals > 0 ? "e.g. 0.5" : "e.g. 5"}
          autoComplete="off"
          inputMode="decimal"
        />
      </label>

      <label style={s.label}>
        Time in Force
        <select value={tif} onChange={(e) => setTif(e.target.value)}>
          <option value={TimeInForce.GoodTillCancel}>GTC</option>
          <option value={TimeInForce.ImmediateOrCancel}>IOC</option>
          <option value={TimeInForce.FillOrKill}>FOK</option>
        </select>
      </label>

      {/* Market display */}
      <div style={s.marketRow}>
        <span style={{ color: "var(--text-muted)", fontSize: 11 }}>Market</span>
        <span style={{ fontWeight: 600 }}>{market || "—"}</span>
      </div>

      {/* Available balance for the side in play */}
      {availableSymbol && (
        <div style={s.marketRow}>
          <span style={{ color: "var(--text-muted)", fontSize: 11 }}>Available</span>
          <span
            style={{
              fontWeight: 600,
              fontFamily: "var(--font-mono)",
              color: insufficientBalance ? "var(--red)" : undefined,
            }}
          >
            {fmtUnits(available, availableDecimals)} {availableSymbol}
          </span>
        </div>
      )}
      {insufficientBalance && needed !== undefined && (
        <span style={s.insufficientHint}>
          Needs {fmtUnits(needed, availableDecimals)} {availableSymbol}
        </span>
      )}

      <button
        type="submit"
        disabled={loading || !market || insufficientBalance}
        style={{ ...s.submitBtn, background: isBuy ? "var(--green)" : "var(--red)" }}
      >
        {loading ? "Submitting…" : `${isBuy ? "Buy" : "Sell"}`}
      </button>
    </form>
  );
}

// ── Styles ────────────────────────────────────────────────────────────────

const s = {
  form: {
    display: "flex",
    flexDirection: "column" as const,
    gap: 10,
    padding: 14,
    borderBottom: "1px solid var(--border)",
    animation: "fade-in 200ms ease",
  },
  header: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    marginBottom: 2,
  },
  title: {
    fontSize: 11,
    fontWeight: 600,
    textTransform: "uppercase" as const,
    letterSpacing: "0.06em",
    color: "var(--text-secondary)",
  },
  sideRow: {
    display: "grid",
    gridTemplateColumns: "1fr 1fr",
    gap: 6,
  },
  sideBtn: {
    padding: "7px 0",
    borderRadius: "var(--radius-sm)",
    fontWeight: 600,
    fontSize: 13,
    transition: "background var(--transition), color var(--transition)",
  } as const,
  buyActive: {
    background: "var(--green)",
    color: "#000",
  } as const,
  sellActive: {
    background: "var(--red)",
    color: "#fff",
  } as const,
  sideInactive: {
    background: "var(--bg-hover)",
    color: "var(--text-secondary)",
  } as const,
  label: {
    display: "flex",
    flexDirection: "column" as const,
    gap: 4,
    fontSize: 11,
    color: "var(--text-secondary)",
    fontWeight: 500,
  },
  marketRow: {
    display: "flex",
    justifyContent: "space-between",
    alignItems: "center",
    padding: "6px 0",
    borderTop: "1px solid var(--border-subtle)",
    fontSize: 12,
  },
  insufficientHint: {
    fontSize: 10,
    color: "var(--red)",
    textAlign: "right" as const,
    marginTop: -6,
  },
  submitBtn: {
    padding: "9px 0",
    borderRadius: "var(--radius-sm)",
    fontWeight: 700,
    fontSize: 14,
    color: "#fff",
    marginTop: 4,
    transition: "filter var(--transition)",
  } as const,
} as const;
