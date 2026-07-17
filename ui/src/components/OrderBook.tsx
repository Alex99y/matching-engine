import type { BookLevel } from "ts-sdk";
import type { MatchingEngineClient } from "ts-sdk";
import { useMarketStream } from "../hooks/useMarketStream.ts";
import { fmtUnits } from "../utils/format.ts";
import { Skeleton, SkeletonRows } from "./Skeleton.tsx";

// ── Sub-components ────────────────────────────────────────────────────────

function BookRow({
  level,
  side,
  maxQty,
  quoteDecimals,
  baseDecimals,
}: {
  level: BookLevel;
  side: "bid" | "ask";
  maxQty: bigint;
  quoteDecimals: number;
  baseDecimals: number;
}) {
  const pct = maxQty > 0n ? Number((level.quantity * 100n) / maxQty) : 0;
  const color = side === "bid" ? "var(--green)" : "var(--red)";
  const bgColor = side === "bid" ? "var(--green-dim)" : "var(--red-dim)";

  return (
    <div style={{ ...s.row, position: "relative" }}>
      {/* Depth bar */}
      <div
        style={{
          position: "absolute",
          top: 0,
          bottom: 0,
          right: 0,
          width: `${pct}%`,
          background: bgColor,
          transition: "width 200ms ease",
        }}
      />
      <span style={{ ...s.cell, color, zIndex: 1 }}>
        {fmtUnits(level.price, quoteDecimals)}
      </span>
      <span style={{ ...s.cell, textAlign: "right", zIndex: 1 }}>
        {fmtUnits(level.quantity, baseDecimals)}
      </span>
    </div>
  );
}

function Spread({
  bestBid,
  bestAsk,
  quoteDecimals,
}: {
  bestBid: bigint | null;
  bestAsk: bigint | null;
  quoteDecimals: number;
}) {
  if (!bestBid || !bestAsk || bestAsk <= bestBid) return null;
  const spread = bestAsk - bestBid;
  return (
    <div style={s.spread}>
      <span style={{ color: "var(--text-muted)", fontSize: 11 }}>
        Spread: {fmtUnits(spread, quoteDecimals)}
      </span>
    </div>
  );
}

// ── Main component ────────────────────────────────────────────────────────

interface Props {
  client: MatchingEngineClient;
  market: string;
  quoteDecimals: number;
  baseDecimals: number;
}

export function OrderBook({ client, market, quoteDecimals, baseDecimals }: Props) {
  const book = useMarketStream(client, market);

  const maxBidQty =
    book.bids.length > 0
      ? book.bids.reduce((m, l) => (l.quantity > m ? l.quantity : m), 0n)
      : 0n;
  const maxAskQty =
    book.asks.length > 0
      ? book.asks.reduce((m, l) => (l.quantity > m ? l.quantity : m), 0n)
      : 0n;

  const bestBid = book.bids[0]?.price ?? null;
  const bestAsk = book.asks[0]?.price ?? null;

  return (
    <div style={s.container}>
      <div style={s.header}>
        <span style={s.title}>Order Book</span>
        <StatusDot status={book.status} />
      </div>

      {/* Column headers */}
      <div style={{ ...s.row, ...s.colHeaders }}>
        <span>Price</span>
        <span style={{ textAlign: "right" }}>Size</span>
      </div>

      {/* Asks — reversed so lowest ask is at the bottom (near spread) */}
      <div style={s.askBlock}>
        {book.status === "connecting" ? (
          <SkeletonRows count={8} gap={2} />
        ) : (
          [...book.asks].reverse().map((l) => (
            <BookRow key={String(l.price)} level={l} side="ask" maxQty={maxAskQty} quoteDecimals={quoteDecimals} baseDecimals={baseDecimals} />
          ))
        )}
      </div>

      {/* Spread */}
      {book.status === "live" && (
        <Spread bestBid={bestBid} bestAsk={bestAsk} quoteDecimals={quoteDecimals} />
      )}

      {/* Last trade price */}
      {book.lastTradePrice !== null && (
        <div
          style={{
            ...s.lastTrade,
            color:
              book.lastTradeSide === "buy" ? "var(--green)" : "var(--red)",
          }}
        >
          {fmtUnits(book.lastTradePrice, quoteDecimals)}
        </div>
      )}

      {/* Bids */}
      <div style={s.bidBlock}>
        {book.status === "connecting" ? (
          <SkeletonRows count={8} gap={2} />
        ) : (
          book.bids.map((l) => (
            <BookRow key={String(l.price)} level={l} side="bid" maxQty={maxBidQty} quoteDecimals={quoteDecimals} baseDecimals={baseDecimals} />
          ))
        )}
      </div>

      {book.status === "error" && (
        <div style={s.error}>{book.error}</div>
      )}
    </div>
  );
}

function StatusDot({ status }: { status: "connecting" | "live" | "error" }) {
  const color =
    status === "live"
      ? "var(--green)"
      : status === "error"
        ? "var(--red)"
        : "var(--text-muted)";
  return (
    <span
      style={{
        display: "inline-block",
        width: 7,
        height: 7,
        borderRadius: "50%",
        background: color,
        boxShadow: status === "live" ? `0 0 6px ${color}` : "none",
        transition: "background 300ms, box-shadow 300ms",
      }}
    />
  );
}

// ── Styles ────────────────────────────────────────────────────────────────

const s = {
  container: {
    display: "flex",
    flexDirection: "column" as const,
    height: "100%",
    background: "var(--bg-panel)",
    borderRight: "1px solid var(--border)",
    overflow: "hidden",
    animation: "fade-in 200ms ease",
  },
  header: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    padding: "10px 12px 6px",
    borderBottom: "1px solid var(--border-subtle)",
  },
  title: {
    fontSize: 11,
    fontWeight: 600,
    textTransform: "uppercase" as const,
    letterSpacing: "0.06em",
    color: "var(--text-secondary)",
  },
  colHeaders: {
    padding: "4px 12px",
    color: "var(--text-muted)",
    fontSize: 10,
    fontWeight: 600,
    letterSpacing: "0.04em",
    textTransform: "uppercase" as const,
    borderBottom: "1px solid var(--border-subtle)",
  },
  askBlock: {
    flex: 1,
    overflowY: "auto" as const,
    display: "flex",
    flexDirection: "column" as const,
    justifyContent: "flex-end",
    padding: "2px 0",
  },
  bidBlock: {
    flex: 1,
    overflowY: "auto" as const,
    padding: "2px 0",
  },
  row: {
    display: "grid",
    gridTemplateColumns: "1fr 1fr",
    padding: "2px 12px",
    fontSize: 12,
    fontFamily: "var(--font-mono)",
    cursor: "default",
    userSelect: "none" as const,
  },
  cell: {
    position: "relative" as const,
  },
  spread: {
    padding: "4px 12px",
    borderTop: "1px solid var(--border-subtle)",
    borderBottom: "1px solid var(--border-subtle)",
    display: "flex",
    justifyContent: "center",
    background: "var(--bg-card)",
  },
  lastTrade: {
    fontFamily: "var(--font-mono)",
    fontWeight: 700,
    fontSize: 14,
    textAlign: "center" as const,
    padding: "4px 12px",
  },
  error: {
    padding: 12,
    color: "var(--red)",
    fontSize: 11,
  },
} as const;
