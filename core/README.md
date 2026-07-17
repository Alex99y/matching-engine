# Matching Engine

The matching engine (ME) is the heart of the exchange. It receives buy and sell orders, pairs compatible ones together, and settles the resulting trades. It is the only component that can move funds between users.

---

## The order book

Every market keeps an **order book**: a list of all resting orders, sorted by price.

- **Bids** — buyers waiting to buy, sorted highest price first.
- **Asks** — sellers waiting to sell, sorted lowest price first.

The gap between the best (highest) bid and the best (lowest) ask is the **spread**. A trade happens when an incoming order's price overlaps with an order on the opposite side.

```
Asks (sell orders)
  $65,020  →  2.0 BTC
  $65,010  →  1.5 BTC   ← best ask
────────────── spread ──
  $65,000  →  0.8 BTC   ← best bid
  $64,990  →  1.2 BTC
Bids (buy orders)
```

---

## How orders are matched

When a new order arrives, the ME checks the opposite side of the book for compatible prices. Matching follows two rules:

1. **Price priority** — the best price is matched first (highest bid, lowest ask).
2. **Time priority** — among orders at the same price, the oldest one is matched first (FIFO).

A new order that can be matched immediately is called a **taker**. The resting order it trades against is the **maker**.

---

## Order types

### Limit order
The trader specifies the price they are willing to pay or accept. The order will only match at that price or better. If it cannot be filled immediately, the unfilled quantity rests in the book and waits.

### Market order
The trader specifies a quantity (or a budget) without a price. The order matches against whatever liquidity is available right now, at the current market price. Market orders never rest in the book.

---

## Time-in-force

Time-in-force controls what happens to the unfilled portion of an order after matching:

| Option | Name | Behaviour |
|--------|------|-----------|
| **GTC** | Good Till Cancel | Rest in the book until filled or explicitly cancelled. |
| **IOC** | Immediate or Cancel | Fill as much as possible right now; cancel the rest. |
| **FOK** | Fill or Kill | Fill the entire quantity at once or reject the order entirely. |

---

## What happens when orders match

When a taker and a maker agree on a price, a **fill** (trade) is recorded:

- The buyer receives the base asset (e.g. BTC).
- The seller receives the quote asset (e.g. USDT).
- A small fee is deducted from the asset each party receives.
- If only part of an order is filled, it becomes **partially filled** and the remainder either rests or is cancelled, depending on the time-in-force.

---

## Fund safety

Before an order is accepted, the ME checks that the user has enough funds:

- A **buy** order blocks the required quote amount (e.g. USDT).
- A **sell** order blocks the required base amount (e.g. BTC).

Blocked funds cannot be used for other orders. When a trade settles, the blocked funds are transferred to the counterparty. If an order is cancelled or only partially filled, any unused blocked amount is released back to the user's available balance.

This reservation-then-settle model guarantees that no order can be placed without the funds to back it, and that no funds move except as the direct result of a confirmed trade.

---

## Order lifecycle

```
Order submitted
      │
      ▼
Funds reserved (blocked)
      │
      ├─ Insufficient funds ──► Rejected (cancelled, funds released)
      │
      ▼
Matching attempted
      │
      ├─ Fully filled ──────────► Settled, funds transferred, order closed
      │
      ├─ Partially filled (GTC) ► Remainder rests in book, waiting
      │
      ├─ Partially filled (IOC) ► Remainder cancelled, unused funds released
      │
      └─ No fill (FOK) ─────────► Entire order cancelled, funds released
```

For the technical implementation of each stage (batching, transactions, recovery) see [ARCHITECTURE.md](ARCHITECTURE.md).
