import { MatchingEngineClient } from "ts-sdk";
import { loadConfig } from "./config.js";
import { logger } from "./logger.js";
import { buildScaleFactors } from "./scale.js";
import type { DepthStream } from "./depth-stream.js";
import { BinanceDepthStream } from "./binance.js";
import { OkxDepthStream } from "./okx.js";
import { BookMirror } from "./mirror.js";

async function main(): Promise<void> {
  const config = loadConfig();

  logger.info("Starting ME liquidity bot", {
    meMarket:    config.meMarket,
    provider:    config.provider,
    symbol:      config.provider === "binance" ? config.binanceSymbol : config.okxInstId,
    botLevels:   config.botLevels,
    depthLevels: config.depthLevels,
  });


  logger.info("Connecting to the ME ...")
  const client = new MatchingEngineClient(config.meHost, config.mePort, {
    allowInsecure: config.meInsecure,
  });

  logger.info(`Logging in with ${config.meUsername}...`)
  let session = await client.login({
    username: config.meUsername,
    password: config.mePassword,
  });
  logger.info("Authenticated with ME", { username: config.meUsername });

  async function reauth() {
    session = await client.login({ username: config.meUsername, password: config.mePassword });
    logger.info("Re-authenticated with ME");
    return session;
  }

  // ── Load market + instruments ────────────────────────────────────────────────

  const [markets, instruments] = await Promise.all([
    client.getMarkets(),
    client.getInstruments(),
  ]);

  const market = markets.find(
    (m) => `${m.baseSymbol}-${m.quoteSymbol}` === config.meMarket,
  );
  if (market === undefined) {
    throw new Error(`Market not found in ME: ${config.meMarket}`);
  }

  const scale = buildScaleFactors(market, instruments);
  logger.info("Market loaded", {
    market:        config.meMarket,
    priceQuantum:  scale.priceQuantum.toString(),
    amountQuantum: scale.amountQuantum.toString(),
    minOrderSize:  scale.minOrderSize.toString(),
    maxOrderSize:  scale.maxOrderSize.toString(),
    baseDecimals:  scale.baseDecimals,
    quoteDecimals: scale.quoteDecimals,
  });

  // ── Build mirror & clean up stale orders from a previous run ────────────────

  const mirror = new BookMirror(config, scale, session, reauth);
  await mirror.cancelAll();

  // ── Start periodic reconcile loop ────────────────────────────────────────────

  mirror.startReconcileLoop();

  // ── Start depth stream ────────────────────────────────────────────────────────

  const stream: DepthStream = config.provider === "binance"
    ? new BinanceDepthStream(
        config.binanceSymbol,
        config.depthLevels,
        (update) => mirror.onDepth(update),
        (err)    => logger.error("Binance stream error", { message: err.message }),
      )
    : new OkxDepthStream(
        config.okxInstId,
        (update) => mirror.onDepth(update),
        (err)    => logger.error("OKX stream error", { message: err.message }),
      );
  stream.start();

  // ── Graceful shutdown ────────────────────────────────────────────────────────

  let shuttingDown = false;

  function shutdown(): void {
    if (shuttingDown) return;
    shuttingDown = true;
    logger.info("Shutting down — cancelling all bot orders…");
    stream.stop();
    mirror.stopReconcileLoop();
    mirror
      .cancelAll()
      .then(() => {
        logger.info("All bot orders cancelled. Goodbye.");
        process.exit(0);
      })
      .catch((err: unknown) => {
        logger.error("Error during shutdown cancel", { message: String(err) });
        process.exit(1);
      });
  }

  process.on("SIGINT",  shutdown);
  process.on("SIGTERM", shutdown);
}

main().catch((err: unknown) => {
  console.error("Fatal:", err);
  process.exit(1);
});
