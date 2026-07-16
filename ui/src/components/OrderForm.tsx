import { useState } from "react";
import { OrderSide, OrderType, TimeInForce } from "ts-sdk";
import { useSession } from "../contexts/AuthContext.tsx";
import { useToast } from "../contexts/ToastContext.tsx";
import { parseBigInt } from "../utils/format.ts";

interface Props {
  market: string;
  onOrderPlaced?: () => void;
}

export function OrderForm({ market, onOrderPlaced }: Props) {
  const { session } = useSession();
  const { showToast } = useToast();

  const [side, setSide] = useState<"buy" | "sell">("buy");
  const [price, setPrice] = useState("");
  const [quantity, setQuantity] = useState("");
  const [tif, setTif] = useState<string>(TimeInForce.GoodTillCancel);
  const [loading, setLoading] = useState(false);

  const isBuy = side === "buy";

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!market) { showToast("Select a market first", "error"); return; }

    const priceBig = parseBigInt(price);
    const qtyBig = parseBigInt(quantity);

    if (priceBig === undefined) { showToast("Invalid price — enter a positive integer", "error"); return; }
    if (qtyBig === undefined || qtyBig === 0n) { showToast("Invalid quantity — enter a positive integer", "error"); return; }

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
        <span style={{ fontSize: 11, color: "var(--text-muted)" }}>
          raw quantum units
        </span>
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
        Price
        <input
          value={price}
          onChange={(e) => setPrice(e.target.value)}
          placeholder="e.g. 2000000"
          autoComplete="off"
        />
      </label>

      <label style={s.label}>
        Quantity
        <input
          value={quantity}
          onChange={(e) => setQuantity(e.target.value)}
          placeholder="e.g. 5"
          autoComplete="off"
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

      <button
        type="submit"
        disabled={loading || !market}
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
