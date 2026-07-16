// Default connection settings; can be overridden by VITE_API_* env vars.
// In practice the user edits them on the login screen before connecting.
export const DEFAULT_HOST = import.meta.env["VITE_API_HOST"] ?? "localhost";
export const DEFAULT_PORT = Number(import.meta.env["VITE_API_PORT"] ?? 4000);
export const DEFAULT_INSECURE = import.meta.env["VITE_API_INSECURE"] !== "false";

// How many book levels to show per side in the order book.
export const BOOK_DEPTH = 15;

// How many historical candles to fetch on load.
export const CANDLE_HISTORY_BARS = 200;
