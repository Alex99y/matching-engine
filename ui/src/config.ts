export type ApiTarget = {
  readonly host: string;
  readonly port: number;
  readonly insecure: boolean;
};

const FALLBACK_API_URL = "http://localhost:4000";
const FALLBACK_API: ApiTarget = { host: "localhost", port: 4000, insecure: true };

// The SDK constructor takes host, port and scheme separately, so a URL has to be taken apart
// before it can be handed over.
function parseApiUrl(raw: string): ApiTarget | null {
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return null;
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") return null;
  if (!url.hostname) return null;

  const insecure = url.protocol === "http:";
  const port = url.port ? Number(url.port) : insecure ? 80 : 443;
  if (!Number.isInteger(port) || port < 1 || port > 65535) return null;

  return { host: url.hostname, port, insecure };
}

// A bad value falls back rather than throwing: this runs at module load, so throwing would
// render a blank page whose only clue is in the console. The login form stays editable, so a
// wrong URL here is recoverable by hand.
function resolveApiTarget(): ApiTarget {
  const configured = import.meta.env["VITE_API_URL"];
  if (configured === undefined || configured.trim() === "") return FALLBACK_API;

  const trimmed = configured.trim();
  const parsed = parseApiUrl(trimmed);
  if (parsed === null) {
    console.error(
      `VITE_API_URL is not a valid http(s) URL: ${configured} — falling back to ${FALLBACK_API_URL}`,
    );
    return FALLBACK_API;
  }

  // The SDK appends /api/v1 itself, so only the origin is used. Worth saying out loud: the
  // e2e suite's own E2E_API_URL does include that path, and copying it here would otherwise
  // look like it worked.
  if (!/^https?:\/\/[^/]+\/?$/.test(trimmed)) {
    console.warn(`VITE_API_URL: ignoring everything after the origin in ${configured}`);
  }
  return parsed;
}

// Connection defaults, overridable with VITE_API_URL (e.g. https://api.example.com).
// In practice the user can still edit them on the login screen before connecting.
export const DEFAULT_API: ApiTarget = resolveApiTarget();

// How many book levels to show per side in the order book.
export const BOOK_DEPTH = 15;

// How many historical candles to fetch on load.
export const CANDLE_HISTORY_BARS = 200;

// GET /markets/prices has no SSE counterpart (unlike the order book), so the
// price ticker polls instead of streaming.
export const PRICE_TICKER_POLL_MS = 10_000;
