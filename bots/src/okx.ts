import { logger } from "./logger.js";
import type { DepthHandler, DepthStream, ErrorHandler } from "./depth-stream.js";

interface RawOkxBooksData {
  asks: [string, string, string, string][];
  bids: [string, string, string, string][];
  seqId: number;
}

interface RawOkxBooksFrame {
  data: RawOkxBooksData[];
}

interface RawOkxEventFrame {
  event: "subscribe" | "unsubscribe" | "error";
  msg?: string;
}

function isRawOkxBooksData(x: unknown): x is RawOkxBooksData {
  return (
    typeof x === "object" &&
    x !== null &&
    "seqId" in x && typeof (x as Record<string, unknown>)["seqId"] === "number" &&
    "bids" in x && Array.isArray((x as Record<string, unknown>)["bids"]) &&
    "asks" in x && Array.isArray((x as Record<string, unknown>)["asks"])
  );
}

function isRawOkxBooksFrame(x: unknown): x is RawOkxBooksFrame {
  return (
    typeof x === "object" &&
    x !== null &&
    "data" in x &&
    Array.isArray((x as Record<string, unknown>)["data"]) &&
    (x as { data: unknown[] }).data.length > 0 &&
    isRawOkxBooksData((x as { data: unknown[] }).data[0])
  );
}

function isRawOkxEventFrame(x: unknown): x is RawOkxEventFrame {
  return (
    typeof x === "object" &&
    x !== null &&
    "event" in x &&
    typeof (x as Record<string, unknown>)["event"] === "string"
  );
}

const BASE_URL = "wss://ws.okx.com:8443/ws/v5/public";
const MAX_BACKOFF_MS = 30_000;
const HEARTBEAT_IDLE_MS = 20_000;
const PONG_TIMEOUT_MS = 5_000;

/**
 * Connects to OKX's books5 WebSocket channel (top-5 snapshot, pushed on change)
 * for one instrument. Automatically reconnects with exponential backoff on
 * disconnect, and drives OKX's text-frame ping/pong keepalive — OKX drops the
 * socket after 30s of silence and does not honor protocol-level WS ping frames.
 */
export class OkxDepthStream implements DepthStream {
  private ws: WebSocket | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private heartbeatTimer: ReturnType<typeof setTimeout> | null = null;
  private pongTimer: ReturnType<typeof setTimeout> | null = null;
  private backoffMs = 1_000;
  private stopped = false;

  constructor(
    private readonly instId: string,
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
    this.clearReconnectTimer();
    this.clearHeartbeatTimers();
    if (this.ws !== null) {
      this.ws.close();
      this.ws = null;
    }
  }

  private connect(): void {
    const ws = new WebSocket(BASE_URL);
    this.ws = ws;

    ws.addEventListener("open", () => {
      this.backoffMs = 1_000;
      logger.info("OKX WS connected", { instId: this.instId });
      ws.send(JSON.stringify({
        op: "subscribe",
        args: [{ channel: "books5", instId: this.instId }],
      }));
      this.resetHeartbeat();
    });

    ws.addEventListener("message", (event) => {
      this.resetHeartbeat();

      const raw = event.data;
      if (typeof raw !== "string") return;
      if (raw === "pong") return;

      let parsed: unknown;
      try {
        parsed = JSON.parse(raw);
      } catch {
        // Malformed frame — ignore and keep the stream alive
        return;
      }

      if (isRawOkxEventFrame(parsed)) {
        if (parsed.event === "error") {
          this.onError(new Error(`OKX WS error for ${this.instId}: ${parsed.msg ?? "unknown"}`));
        } else {
          logger.info("OKX WS subscription ack", { instId: this.instId, event: parsed.event });
        }
        return;
      }

      if (!isRawOkxBooksFrame(parsed)) return;
      const frame = parsed.data[0];
      if (frame === undefined) return;

      this.onDepth({
        lastUpdateId: frame.seqId,
        bids: frame.bids.map(([price, qty]) => [price, qty] as const),
        asks: frame.asks.map(([price, qty]) => [price, qty] as const),
      });
    });

    ws.addEventListener("error", () => {
      this.onError(new Error(`OKX WS error for ${this.instId}`));
    });

    ws.addEventListener("close", (event) => {
      logger.warn("OKX WS closed", { code: event.code });
      this.ws = null;
      this.clearHeartbeatTimers();
      this.scheduleReconnect();
    });
  }

  private resetHeartbeat(): void {
    this.clearHeartbeatTimers();
    this.heartbeatTimer = setTimeout(() => {
      this.ws?.send("ping");
      this.pongTimer = setTimeout(() => {
        logger.warn("OKX WS heartbeat timeout, forcing reconnect", { instId: this.instId });
        this.ws?.close();
      }, PONG_TIMEOUT_MS);
    }, HEARTBEAT_IDLE_MS);
  }

  private clearHeartbeatTimers(): void {
    if (this.heartbeatTimer !== null) {
      clearTimeout(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
    if (this.pongTimer !== null) {
      clearTimeout(this.pongTimer);
      this.pongTimer = null;
    }
  }

  private clearReconnectTimer(): void {
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }

  private scheduleReconnect(): void {
    if (this.stopped) return;
    logger.info(`Reconnecting in ${this.backoffMs}ms`, { instId: this.instId });
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      if (!this.stopped) this.connect();
    }, this.backoffMs);
    this.backoffMs = Math.min(this.backoffMs * 2, MAX_BACKOFF_MS);
  }
}
