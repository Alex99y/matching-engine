export interface BotConfig {
  readonly meHost: string;
  readonly mePort: number;
  readonly meInsecure: boolean;
  readonly meUsername: string;
  readonly mePassword: string;
  readonly meMarket: string;
  readonly binanceSymbol: string;
  readonly depthLevels: 5 | 10 | 20;
  readonly botLevels: number;
  readonly priceTolerance: number;
  readonly qtyTolerance: number;
  readonly reconcileIntervalMs: number;
  readonly cooldownMs: number;
  readonly dryRun: boolean;
}

function requireEnv(key: string): string {
  const val = process.env[key];
  if (!val) throw new Error(`Missing required env var: ${key}`);
  return val;
}

function optionalInt(key: string, fallback: number): number {
  const val = process.env[key];
  if (!val) return fallback;
  const n = parseInt(val, 10);
  if (!Number.isFinite(n)) throw new Error(`${key} must be an integer, got: ${val}`);
  return n;
}

function optionalFloat(key: string, fallback: number): number {
  const val = process.env[key];
  if (!val) return fallback;
  const n = parseFloat(val);
  if (!Number.isFinite(n)) throw new Error(`${key} must be a number, got: ${val}`);
  return n;
}

export function loadConfig(): BotConfig {
  const depthRaw = optionalInt("BINANCE_DEPTH_LEVELS", 20);
  if (depthRaw !== 5 && depthRaw !== 10 && depthRaw !== 20) {
    throw new Error("BINANCE_DEPTH_LEVELS must be 5, 10, or 20");
  }

  const botLevels = optionalInt("BOT_LEVELS", 5);
  if (botLevels < 1 || botLevels > depthRaw) {
    throw new Error(`BOT_LEVELS must be between 1 and BINANCE_DEPTH_LEVELS (${depthRaw})`);
  }

  return {
    meHost:              process.env["ME_HOST"] ?? "localhost",
    mePort:              optionalInt("ME_PORT", 4000),
    meInsecure:          (process.env["ME_INSECURE"] ?? "true") !== "false",
    meUsername:          requireEnv("ME_USERNAME"),
    mePassword:          requireEnv("ME_PASSWORD"),
    meMarket:            requireEnv("ME_MARKET"),
    binanceSymbol:       requireEnv("BINANCE_SYMBOL").toLowerCase(),
    depthLevels:         depthRaw as 5 | 10 | 20,
    botLevels,
    priceTolerance:      optionalFloat("BOT_PRICE_TOLERANCE", 0.0005),
    qtyTolerance:        optionalFloat("BOT_QTY_TOLERANCE", 0.20),
    reconcileIntervalMs: optionalInt("BOT_RECONCILE_INTERVAL_MS", 2000),
    cooldownMs:          optionalInt("BOT_COOLDOWN_MS", 500),
    dryRun:              (process.env["BOT_DRY_RUN"] ?? "false") === "true",
  };
}
