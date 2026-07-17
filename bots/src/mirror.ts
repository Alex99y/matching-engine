import type { AuthenticatedClient, BatchCreateOrderResult, CreateOrderParams } from "ts-sdk";
import { APIError, AuthenticationError, OrderSide, OrderType, TimeInForce } from "ts-sdk";
import type { BotConfig } from "./config.js";
import type { DepthUpdate } from "./binance.js";
import { logger } from "./logger.js";
import { capQtyToNotional, toMePrice, toMeQty, type ScaleFactors } from "./scale.js";

// ── Types ────────────────────────────────────────────────────────────────────

type SlotKey = `${"bid" | "ask"}-${number}`;

interface ActiveOrder {
  readonly orderId: string;
  readonly price: bigint;
  readonly qty: bigint;
}

interface PendingPlacement {
  readonly key: SlotKey;
  readonly side: "buy" | "sell";
  readonly price: bigint;
  readonly qty: bigint;
}

// ── BookMirror ───────────────────────────────────────────────────────────────

/**
 * Reconciles the Binance order book snapshot against the bot's open ME orders.
 * On each depth tick it computes which slots are stale (price/qty moved beyond
 * tolerance), cancels them in batch, and places updated orders in batch.
 * A periodic poll loop catches any fills or external cancellations.
 */
export class BookMirror {
  private activeOrders = new Map<SlotKey, ActiveOrder>();
  private reconciling = false;
  private cooldownUntil = 0;
  private pendingDepth: DepthUpdate | null = null;
  private reconcileTimer: ReturnType<typeof setInterval> | null = null;

  constructor(
    private readonly config: BotConfig,
    private readonly scale: ScaleFactors,
    private session: AuthenticatedClient,
    private readonly reauth: () => Promise<AuthenticatedClient>,
  ) {}

  // ── Public API ─────────────────────────────────────────────────────────────

  onDepth(update: DepthUpdate): void {
    const now = Date.now();
    console.log(update)
    if (this.reconciling || now < this.cooldownUntil) {
      this.pendingDepth = update;
      return;
    }
    this.cooldownUntil = now + this.config.cooldownMs;
    void this.reconcile(update);
  }

  startReconcileLoop(): void {
    this.reconcileTimer = setInterval(() => {
      void this.syncFromME();
    }, this.config.reconcileIntervalMs);
  }

  stopReconcileLoop(): void {
    if (this.reconcileTimer !== null) {
      clearInterval(this.reconcileTimer);
      this.reconcileTimer = null;
    }
  }

  /** Cancel all tracked orders + any open orders found on the ME for this market. */
  async cancelAll(): Promise<void> {
    if (this.config.dryRun) {
      logger.info("[DRY-RUN] skipping cancelAll");
      return;
    }
    const localIds = [...this.activeOrders.values()].map((o) => o.orderId);

    let remoteIds: string[] = [];
    try {
      const open = await this.withReauth(() =>
        this.session.getOrders({ showOpen: true, market: this.config.meMarket, limit: 100 }),
      );
      remoteIds = open.map((o) => o.id);
    } catch (err) {
      logger.warn("Could not fetch existing orders for cleanup", { message: String(err) });
    }

    const ids = [...new Set([...localIds, ...remoteIds])];
    if (ids.length === 0) {
      logger.info("No existing orders to cancel");
      return;
    }

    logger.info("Cancelling existing orders", { count: ids.length });
    try {
      const { results } = await this.withReauth(() => this.session.cancelOrders(ids));
      const failed = results.filter((r) => r.error !== undefined);
      if (failed.length > 0) {
        logger.warn("Some cancellations reported errors (likely already filled)", {
          count: failed.length,
        });
      }
    } catch (err) {
      logger.error("cancelAll failed", { message: String(err) });
    }
    this.activeOrders.clear();
  }

  // ── Private ────────────────────────────────────────────────────────────────

  /** Remove from local state any orders that are no longer open on the ME. */
  private async syncFromME(): Promise<void> {
    if (this.config.dryRun || this.reconciling) return;
    try {
      const open = await this.withReauth(() =>
        this.session.getOrders({ showOpen: true, market: this.config.meMarket, limit: 100 }),
      );
      const openIds = new Set(open.map((o) => o.id));
      for (const [key, slot] of this.activeOrders) {
        if (!openIds.has(slot.orderId)) {
          logger.debug("Slot filled or cancelled externally, freeing", {
            key,
            orderId: slot.orderId,
          });
          this.activeOrders.delete(key);
        }
      }
    } catch (err) {
      logger.error("syncFromME failed", { message: String(err) });
    }
  }

  private async reconcile(depth: DepthUpdate): Promise<void> {
    this.reconciling = true;
    try {
      await this.doReconcile(depth);
    } catch (err) {
      logger.error("Reconcile error", { message: String(err) });
    } finally {
      this.reconciling = false;
      const pending = this.pendingDepth;
      if (pending !== null) {
        this.pendingDepth = null;
        // Apply the latest accumulated depth without another cooldown delay.
        void this.reconcile(pending);
      }
    }
  }

  private async doReconcile(depth: DepthUpdate): Promise<void> {
    const n = Math.min(this.config.botLevels, depth.bids.length, depth.asks.length);

    const toCancel: string[] = [];
    let toPlace: PendingPlacement[] = [];

    const sides = [
      { prefix: "bid" as const, orderSide: "buy"  as const, levels: depth.bids },
      { prefix: "ask" as const, orderSide: "sell" as const, levels: depth.asks },
    ];

    for (const { prefix, orderSide, levels } of sides) {
      for (let i = 0; i < n; i++) {
        const entry = levels[i];
        if (entry === undefined) continue;

        const mePrice = toMePrice(entry[0], this.scale);
        const rawQty  = toMeQty(entry[1], this.scale);
        if (mePrice === null || rawQty === null) {
          logger.debug("Slot skipped: price or qty out of range", {
            key: `${prefix}-${i}`, binancePrice: entry[0], binanceQty: entry[1],
            mePrice: String(mePrice), rawQty: String(rawQty),
          });
          continue;
        }
        const meQty = capQtyToNotional(mePrice, rawQty, this.scale);
        if (meQty === null) {
          logger.debug("Slot skipped: qty capped below minOrderSize", {
            key: `${prefix}-${i}`, mePrice: mePrice.toString(), rawQty: rawQty.toString(),
          });
          continue;
        }

        const key: SlotKey = `${prefix}-${i}`;
        const existing = this.activeOrders.get(key);

        if (existing !== undefined && this.withinTolerance(existing, mePrice, meQty)) {
          logger.debug("Slot within tolerance, skipping", { key, existingPrice: existing.price.toString(), newPrice: mePrice.toString() });
          continue; // price and qty still within tolerance — leave the order in place
        }

        if (existing !== undefined) {
          toCancel.push(existing.orderId);
        }
        console.log("toPlace", { key, side: orderSide, price: mePrice.toString(), qty: meQty.toString() });
        toPlace.push({ key, side: orderSide, price: mePrice, qty: meQty });
      }
    }

    // ── Self-trade guard ───────────────────────────────────────────────────
    // Two root causes:
    //   (a) Binance depth can temporarily show bid[N] > ask[0] during a fast
    //       price drop — bid levels lag while asks update instantly.
    //   (b) A new bid can land above a stale ask placed in a *previous*
    //       reconcile that is still within tolerance (so not being cancelled).
    // In both cases the ME immediately self-matches the order.
    // We prevent this by treating ALL currently-active asks/bids (stale +
    // new) as the reference boundary when vetting new placements.
    {
      const stGuard = new Set(toCancel);

      // Collect the floor/ceiling from orders that will stay in the ME
      // (active orders not already being cancelled in this reconcile).
      let minStaleAsk: bigint | null = null;
      let maxStaleBid: bigint | null = null;
      for (const [key, slot] of this.activeOrders) {
        if (stGuard.has(slot.orderId)) continue;
        if (key.startsWith("ask-")) {
          if (minStaleAsk === null || slot.price < minStaleAsk) minStaleAsk = slot.price;
        } else if (key.startsWith("bid-")) {
          if (maxStaleBid === null || slot.price > maxStaleBid) maxStaleBid = slot.price;
        }
      }

      // Effective ask floor = min(new asks being placed, stale asks staying).
      const minNewAsk = toPlace
        .filter((p) => p.side === "sell")
        .reduce<bigint | null>((m, p) => (m === null || p.price < m ? p.price : m), null);
      const askFloor: bigint | null =
        minStaleAsk !== null && (minNewAsk === null || minStaleAsk < minNewAsk)
          ? minStaleAsk
          : minNewAsk;

      // Step 1 — drop new bids that would cross any ask (new or stale)
      if (askFloor !== null) {
        const cap = askFloor;
        toPlace = toPlace.filter((p) => {
          if (p.side === "buy" && p.price >= cap) {
            logger.debug("Self-trade guard: skipping new bid above ask floor", {
              key: p.key, bidPrice: p.price.toString(), askFloor: cap.toString(),
            });
            return false;
          }
          return true;
        });
      }

      // Bid ceiling = max of NEW bids only.
      // Stale bids are intentionally excluded: if a stale high bid (from a previous
      // reconcile that the market has since moved away from) were included, it would
      // inflate bidCeil, cause new asks near the current price to be dropped in step 2,
      // and then finalMinAsk (step 4) would land above the stale bid — so step 4 would
      // never cancel it. The result is a permanent stale-bid that blocks asks forever.
      // Step 4 already cancels stale bids that cross surviving new asks, so including
      // stale bids in bidCeil is both redundant and harmful.
      const maxNewBid = toPlace
        .filter((p) => p.side === "buy")
        .reduce<bigint | null>((m, p) => (m === null || p.price > m ? p.price : m), null);
      const bidCeil: bigint | null = maxNewBid;

      // Step 2 — drop new asks that would cross any bid (new or stale)
      if (bidCeil !== null) {
        const cap = bidCeil;
        toPlace = toPlace.filter((p) => {
          if (p.side === "sell" && p.price <= cap) {
            logger.debug("Self-trade guard: skipping new ask below bid ceiling", {
              key: p.key, askPrice: p.price.toString(), bidCeil: cap.toString(),
            });
            return false;
          }
          return true;
        });
      }

      // Step 3 — cancel stale asks that cross remaining new bids
      const finalMaxBid = toPlace
        .filter((p) => p.side === "buy")
        .reduce<bigint | null>((m, p) => (m === null || p.price > m ? p.price : m), null);
      if (finalMaxBid !== null) {
        for (const [key, slot] of this.activeOrders) {
          if (stGuard.has(slot.orderId)) continue;
          if (key.startsWith("ask-") && slot.price <= finalMaxBid) {
            logger.debug("Self-trade guard: cancelling stale ask below new bid", {
              key, askPrice: slot.price.toString(), maxBid: finalMaxBid.toString(),
            });
            toCancel.push(slot.orderId);
            stGuard.add(slot.orderId);
          }
        }
      }

      // Step 4 — cancel stale bids that cross remaining new asks
      const finalMinAsk = toPlace
        .filter((p) => p.side === "sell")
        .reduce<bigint | null>((m, p) => (m === null || p.price < m ? p.price : m), null);
      if (finalMinAsk !== null) {
        for (const [key, slot] of this.activeOrders) {
          if (stGuard.has(slot.orderId)) continue;
          if (key.startsWith("bid-") && slot.price >= finalMinAsk) {
            logger.debug("Self-trade guard: cancelling stale bid above new ask", {
              key, bidPrice: slot.price.toString(), minAsk: finalMinAsk.toString(),
            });
            toCancel.push(slot.orderId);
            stGuard.add(slot.orderId);
          }
        }
      }
    }

    if (this.config.dryRun) {
      for (const id of toCancel) {
        logger.info("[DRY-RUN] would cancel order", { orderId: id });
      }
      for (const p of toPlace) {
        logger.info("[DRY-RUN] would place order", {
          key:   p.key,
          side:  p.side,
          price: p.price.toString(),
          qty:   p.qty.toString(),
        });
      }
      return;
    }

    if (toCancel.length > 0) {
      const { results } = await this.withReauth(() => this.session.cancelOrders(toCancel));
      for (const r of results) {
        if (r.error !== undefined) {
          logger.debug("Cancel reported error (likely already filled)", {
            orderId: r.orderId,
            error: r.error,
          });
        }
      }
      // Remove cancelled slots from local state regardless of per-item errors.
      const cancelledSet = new Set(toCancel);
      for (const [key, slot] of this.activeOrders) {
        if (cancelledSet.has(slot.orderId)) this.activeOrders.delete(key);
      }
    }

    if (toPlace.length === 0) return;

    const params: CreateOrderParams[] = toPlace.map(({ side, price, qty }) => ({
      market:      this.config.meMarket,
      side:        side === "buy" ? OrderSide.Buy : OrderSide.Sell,
      type:        OrderType.Limit,
      timeInForce: TimeInForce.GoodTillCancel,
      price,
      quantity: qty,
    }));

    let results: readonly BatchCreateOrderResult[];
    try {
      ({ results } = await this.withReauth(() => this.session.createOrders(params)));
    } catch (err) {
      if (err instanceof APIError && err.status === 422) {
        // ME rejected every order in the batch (all failed validation).
        // Log and move on — the next reconcile will retry.
        logger.warn("All orders rejected by ME (422)", { count: toPlace.length });
        return;
      }
      throw err;
    }

    for (let i = 0; i < results.length; i++) {
      const res     = results[i];
      const pending = toPlace[i];
      if (res === undefined || pending === undefined) continue;

      if (res.error !== undefined) {
        logger.warn("Order placement failed", { key: pending.key, error: res.error });
        continue;
      }
      if (res.orderId !== undefined) {
        this.activeOrders.set(pending.key, {
          orderId: res.orderId,
          price:   pending.price,
          qty:     pending.qty,
        });
        logger.debug("Order placed", { key: pending.key, orderId: res.orderId });
      }
    }

    const placedCount = toPlace.length - results.filter((r) => r.error !== undefined).length;
    if (placedCount > 0 || toCancel.length > 0) {
      logger.info("Reconcile complete", {
        cancelled: toCancel.length,
        placed: placedCount,
        activeSlots: this.activeOrders.size,
      });
    }
  }

  private withinTolerance(existing: ActiveOrder, newPrice: bigint, newQty: bigint): boolean {
    const priceDiff =
      Number(newPrice > existing.price ? newPrice - existing.price : existing.price - newPrice) /
      Number(existing.price);
    const qtyDiff =
      Number(newQty > existing.qty ? newQty - existing.qty : existing.qty - newQty) /
      Number(existing.qty);
    return priceDiff < this.config.priceTolerance && qtyDiff < this.config.qtyTolerance;
  }

  private async withReauth<T>(fn: () => Promise<T>): Promise<T> {
    try {
      return await fn();
    } catch (err) {
      if (err instanceof AuthenticationError) {
        logger.warn("Session expired, re-authenticating…");
        this.session = await this.reauth();
        return fn();
      }
      throw err;
    }
  }
}
