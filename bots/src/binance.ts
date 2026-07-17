import { logger } from "./logger.js";

export interface DepthUpdate {
  readonly lastUpdateId: number;
  readonly bids: readonly [string, string][];
  readonly asks: readonly [string, string][];
}

type DepthHandler = (update: DepthUpdate) => void;
type ErrorHandler = (err: Error) => void;

interface RawDepthFrame {
  lastUpdateId: number;
  bids: [string, string][];
  asks: [string, string][];
}

function isRawDepthFrame(x: unknown): x is RawDepthFrame {
  return (
    typeof x === "object" &&
    x !== null &&
    "lastUpdateId" in x &&
    typeof (x as Record<string, unknown>)["lastUpdateId"] === "number" &&
    "bids" in x && Array.isArray((x as Record<string, unknown>)["bids"]) &&
    "asks" in x && Array.isArray((x as Record<string, unknown>)["asks"])
  );
}

const BASE_URL = "wss://stream.binance.com:9443/ws";
const MAX_BACKOFF_MS = 30_000;

/**
 * Connects to Binance's partial-depth WebSocket stream for one symbol.
 * Automatically reconnects with exponential backoff on disconnect.
 * Uses Node's native globalThis.WebSocket (Node ≥ 22).
 */
export class BinanceDepthStream {
  private ws: WebSocket | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private backoffMs = 1_000;
  private stopped = false;

  constructor(
    private readonly symbol: string,
    private readonly levels: 5 | 10 | 20,
    private readonly onDepth: DepthHandler,
    private readonly onError: ErrorHandler,
  ) {}

  start(): void {
    this.stopped = false;
    this.backoffMs = 1_000;
    this.connect();
  }

  stop(): void {
    this.stopped = true;
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws !== null) {
      this.ws.close();
      this.ws = null;
    }
  }

  private connect(): void {
    const url = `${BASE_URL}/${this.symbol}@depth${this.levels}@100ms`;
    const ws = new WebSocket(url);
    this.ws = ws;

    ws.addEventListener("open", () => {
      this.backoffMs = 1_000;
      logger.info("Binance WS connected", { symbol: this.symbol, levels: this.levels });
    });

    ws.addEventListener("message", (event) => {
      const raw = event.data;
      if (typeof raw !== "string") return;
      try {
        const parsed: unknown = JSON.parse(raw);
        if (!isRawDepthFrame(parsed)) return;
        this.onDepth({
          lastUpdateId: parsed.lastUpdateId,
          bids: parsed.bids,
          asks: parsed.asks,
        });
      } catch {
        // Malformed frame — ignore and keep the stream alive
      }
    });

    ws.addEventListener("error", () => {
      this.onError(new Error(`Binance WS error for ${this.symbol}`));
    });

    ws.addEventListener("close", (event) => {
      logger.warn("Binance WS closed", { code: event.code });
      this.ws = null;
      this.scheduleReconnect();
    });
  }

  private scheduleReconnect(): void {
    if (this.stopped) return;
    logger.info(`Reconnecting in ${this.backoffMs}ms`, { symbol: this.symbol });
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      if (!this.stopped) this.connect();
    }, this.backoffMs);
    this.backoffMs = Math.min(this.backoffMs * 2, MAX_BACKOFF_MS);
  }
}
