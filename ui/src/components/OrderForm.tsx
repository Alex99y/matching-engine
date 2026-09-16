import { useState } from "react";
import {
  OrderSide,
  OrderType,
  TimeInForce,
  type CreateOrderParams,
} from "ts-sdk";
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

type TifValue = (typeof TimeInForce)[keyof typeof TimeInForce];

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
  const [orderType, setOrderType] = useState<"limit" | "market">("limit");
  const [price, setPrice] = useState("");
  const [quantity, setQuantity] = useState(""); // base qty: limit buy/sell, market sell
  const [total, setTotal] = useState(""); // quote budget: market buy only
  const [tif, setTif] = useState<TifValue>(TimeInForce.GoodTillCancel);
  const [postOnly, setPostOnly] = useState(false);
  // Bracket: once the order fills, the engine arms an opposite-side market exit for what it
  // received and fires it at either trigger. Either or both may be set.
  const [bracket, setBracket] = useState(false);
  const [takeProfit, setTakeProfit] = useState("");
  const [stopLoss, setStopLoss] = useState("");
  const [loading, setLoading] = useState(false);

  const isBuy = side === "buy";
  const isMarket = orderType === "market";
  // A market buy is quote-denominated (the user commits a spend budget); every other
  // combination is base-denominated. Mirrors ValidateOrderEvent server-side.
  const quoteDenominated = isMarket && isBuy;
  // post_only is only accepted for a resting order — limit GTC (see ValidateOrderEvent).
  const postOnlyEligible = !isMarket && tif === TimeInForce.GoodTillCancel;
  const effectivePostOnly = postOnly && postOnlyEligible;

  // A buy spends quote asset; a sell spends base asset.
  const availableSymbol = isBuy ? quoteSymbol : baseSymbol;
  const availableDecimals = isBuy ? quoteDecimals : baseDecimals;
  const available =
    balances.find((b) => b.symbol === availableSymbol)?.balance ?? 0n;

  const priceBig = parseUnits(price, quoteDecimals);
  const qtyBig = parseUnits(quantity, baseDecimals);
  const totalBig = parseUnits(total, quoteDecimals);
  const takeProfitBig = parseUnits(takeProfit, quoteDecimals);
  const stopLossBig = parseUnits(stopLoss, quoteDecimals);

  // Funds the order will reserve. For a limit buy the notional must be normalized by
  // the base scale, exactly like core's quoteAmount() (see ui/CLAUDE.md rule 4) — this
  // is the "Needs 64,500,000,000 USDT" bug if the division is dropped.
  let needed: bigint | undefined;
  if (quoteDenominated) {
    needed = totalBig;
  } else if (isMarket) {
    needed = qtyBig; // market sell: base offered
  } else if (isBuy) {
    needed =
      priceBig !== undefined && qtyBig !== undefined
        ? (priceBig * qtyBig) / 10n ** BigInt(baseDecimals)
        : undefined;
  } else {
    needed = qtyBig; // limit sell: base offered
  }
  // Client-side UX guard only — the server still enforces the real balance check.
  const insufficientBalance = needed !== undefined && needed > available;

  function selectOrderType(next: "limit" | "market") {
    setOrderType(next);
    // market + GTC is rejected server-side; a market order can only be IOC or FOK.
    if (next === "market" && tif === TimeInForce.GoodTillCancel) {
      setTif(TimeInForce.ImmediateOrCancel);
    }
  }

  // Mirrors the API's bracket rules so the user gets a specific message instead of a
  // generic rejection: the exit sits on the opposite side, so a buy takes profit above its
  // price and stops below; a sell is mirrored; a market entry has no price to anchor to.
  function buildTriggers(
    limitPrice: bigint | undefined,
  ): Pick<CreateOrderParams, "takeProfitPrice" | "stopLossPrice"> | string {
    if (!bracket) return {};
    if (takeProfit.trim() === "" && stopLoss.trim() === "") {
      return "Enter a take-profit and/or stop-loss price, or untick the bracket";
    }
    if (takeProfit.trim() !== "" && (takeProfitBig === undefined || takeProfitBig === 0n)) {
      return `Invalid take-profit price — enter a decimal with up to ${quoteDecimals} places`;
    }
    if (stopLoss.trim() !== "" && (stopLossBig === undefined || stopLossBig === 0n)) {
      return `Invalid stop-loss price — enter a decimal with up to ${quoteDecimals} places`;
    }

    const tp = takeProfitBig;
    const sl = stopLossBig;
    const above = isBuy ? tp : sl;
    const below = isBuy ? sl : tp;
    const aboveName = isBuy ? "Take profit" : "Stop loss";
    const belowName = isBuy ? "Stop loss" : "Take profit";
    if (above !== undefined && below !== undefined && below >= above) {
      return `${aboveName} must be above ${belowName.toLowerCase()} for a ${side}`;
    }
    if (limitPrice !== undefined) {
      if (above !== undefined && above <= limitPrice) {
        return `${aboveName} must be above the limit price for a ${side}`;
      }
      if (below !== undefined && below >= limitPrice) {
        return `${belowName} must be below the limit price for a ${side}`;
      }
    }
    return {
      ...(tp !== undefined ? { takeProfitPrice: tp } : {}),
      ...(sl !== undefined ? { stopLossPrice: sl } : {}),
    };
  }

  function buildParams(): CreateOrderParams | string {
    const common = {
      market,
      side: isBuy ? OrderSide.Buy : OrderSide.Sell,
      timeInForce: tif,
    };

    if (quoteDenominated) {
      if (totalBig === undefined || totalBig === 0n) {
        return `Invalid total — enter a decimal with up to ${quoteDecimals} places`;
      }
      const triggers = buildTriggers(undefined);
      if (typeof triggers === "string") return triggers;
      return { ...common, type: OrderType.Market, quoteQty: totalBig, ...triggers };
    }

    if (isMarket) {
      if (qtyBig === undefined || qtyBig === 0n) {
        return `Invalid quantity — enter a decimal with up to ${baseDecimals} places`;
      }
      const triggers = buildTriggers(undefined);
      if (typeof triggers === "string") return triggers;
      return { ...common, type: OrderType.Market, quantity: qtyBig, ...triggers };
    }

    if (priceBig === undefined || priceBig === 0n) {
      return `Invalid price — enter a decimal with up to ${quoteDecimals} places`;
    }
    if (qtyBig === undefined || qtyBig === 0n) {
      return `Invalid quantity — enter a decimal with up to ${baseDecimals} places`;
    }
    const triggers = buildTriggers(priceBig);
    if (typeof triggers === "string") return triggers;
    return {
      ...common,
      type: OrderType.Limit,
      price: priceBig,
      quantity: qtyBig,
      ...(effectivePostOnly ? { postOnly: true } : {}),
      ...triggers,
    };
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!market) {
      showToast("Select a market first", "error");
      return;
    }

    const params = buildParams();
    if (typeof params === "string") {
      showToast(params, "error");
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
      const { results } = await session.createOrders([params]);
      const result = results[0];
      if (result?.error) {
        showToast(`Order rejected: ${result.error}`, "error");
      } else {
        const exit = result?.exitOrderId
          ? ` · exit: ${result.exitOrderId.slice(0, 8)}…`
          : "";
        showToast(
          `Order placed — id: ${result?.orderId?.slice(0, 8) ?? "?"}…${exit}`,
          "success",
        );
        setPrice("");
        setQuantity("");
        setTotal("");
        setTakeProfit("");
        setStopLoss("");
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

      {/* Order-type toggle */}
      <div style={s.sideRow}>
        <button
          type="button"
          onClick={() => selectOrderType("limit")}
          style={{ ...s.typeBtn, ...(!isMarket ? s.typeActive : s.typeInactive) }}
        >
          Limit
        </button>
        <button
          type="button"
          onClick={() => selectOrderType("market")}
          style={{ ...s.typeBtn, ...(isMarket ? s.typeActive : s.typeInactive) }}
        >
          Market
        </button>
      </div>

      {isMarket && (
        <span style={s.hint}>Fills immediately at the best available price.</span>
      )}

      {/* Fields */}
      {!isMarket && (
        <label style={s.label}>
          Price {quoteSymbol && `(${quoteSymbol})`}
          <input
            value={price}
            onChange={(e) => setPrice(e.target.value)}
            placeholder={
              quoteDecimals > 0
                ? `e.g. 63448.${"0".repeat(Math.min(quoteDecimals, 2))}`
                : "e.g. 63448"
            }
            autoComplete="off"
            inputMode="decimal"
          />
        </label>
      )}

      {quoteDenominated ? (
        <label style={s.label}>
          Total {quoteSymbol && `(${quoteSymbol})`}
          <input
            value={total}
            onChange={(e) => setTotal(e.target.value)}
            placeholder={quoteDecimals > 0 ? "e.g. 500.00" : "e.g. 500"}
            autoComplete="off"
            inputMode="decimal"
          />
        </label>
      ) : (
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
      )}

      <label style={s.label}>
        Time in Force
        <select
          value={tif}
          onChange={(e) => setTif(e.target.value as TifValue)}
        >
          {!isMarket && (
            <option value={TimeInForce.GoodTillCancel}>GTC</option>
          )}
          <option value={TimeInForce.ImmediateOrCancel}>IOC</option>
          <option value={TimeInForce.FillOrKill}>FOK</option>
        </select>
      </label>

      <label
        style={{ ...s.checkRow, opacity: postOnlyEligible ? 1 : 0.5 }}
        title="Only for limit GTC orders — rejects the order instead of taking liquidity"
      >
        <input
          type="checkbox"
          checked={effectivePostOnly}
          disabled={!postOnlyEligible}
          onChange={(e) => setPostOnly(e.target.checked)}
          style={s.checkbox}
        />
        Post-only{!postOnlyEligible && " (limit GTC only)"}
      </label>

      <label
        style={s.checkRow}
        title="Once this order fills, a market exit for what it received is armed and fires at either trigger"
      >
        <input
          type="checkbox"
          checked={bracket}
          onChange={(e) => setBracket(e.target.checked)}
          style={s.checkbox}
        />
        Take profit / Stop loss
      </label>

      {bracket && (
        <div style={s.bracket}>
          <label style={s.label}>
            Take profit {quoteSymbol && `(${quoteSymbol})`}
            <input
              value={takeProfit}
              onChange={(e) => setTakeProfit(e.target.value)}
              placeholder={isBuy ? "above the entry price" : "below the entry price"}
              autoComplete="off"
              inputMode="decimal"
            />
          </label>
          <label style={s.label}>
            Stop loss {quoteSymbol && `(${quoteSymbol})`}
            <input
              value={stopLoss}
              onChange={(e) => setStopLoss(e.target.value)}
              placeholder={isBuy ? "below the entry price" : "above the entry price"}
              autoComplete="off"
              inputMode="decimal"
            />
          </label>
          <span style={s.hint}>
            Set one or both. The exit {isBuy ? "sells" : "buys back"} what this order
            receives, at market, when the last trade reaches a trigger.
          </span>
        </div>
      )}

      {/* Market display */}
      <div style={s.marketRow}>
        <span style={{ color: "var(--text-muted)", fontSize: 11 }}>Market</span>
        <span style={{ fontWeight: 600 }}>{market || "—"}</span>
      </div>

      {/* Available balance for the side in play */}
      {availableSymbol && (
        <div style={s.marketRow}>
          <span style={{ color: "var(--text-muted)", fontSize: 11 }}>
            Available
          </span>
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
        style={{
          ...s.submitBtn,
          background: isBuy ? "var(--green)" : "var(--red)",
        }}
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
  typeBtn: {
    padding: "6px 0",
    borderRadius: "var(--radius-sm)",
    fontWeight: 600,
    fontSize: 12,
    transition: "background var(--transition), color var(--transition)",
  } as const,
  typeActive: {
    background: "var(--accent-dim)",
    color: "var(--accent-hover)",
  } as const,
  typeInactive: {
    background: "var(--bg-hover)",
    color: "var(--text-secondary)",
  } as const,
  hint: {
    fontSize: 10,
    color: "var(--text-muted)",
    marginTop: -4,
  },
  label: {
    display: "flex",
    flexDirection: "column" as const,
    gap: 4,
    fontSize: 11,
    color: "var(--text-secondary)",
    fontWeight: 500,
  },
  checkRow: {
    display: "flex",
    alignItems: "center",
    gap: 7,
    fontSize: 11,
    color: "var(--text-secondary)",
    fontWeight: 500,
    cursor: "pointer",
  },
  checkbox: {
    width: 14,
    height: 14,
    flex: "0 0 auto",
    accentColor: "var(--green)",
    cursor: "pointer",
  },
  bracket: {
    display: "flex",
    flexDirection: "column" as const,
    gap: 8,
    padding: "8px 10px",
    borderLeft: "2px solid var(--accent-dim)",
    animation: "fade-in 200ms ease",
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
