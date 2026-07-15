import { describe, expect, it, vi } from "vitest";
import { ParseError, ValidationError } from "../errors/index.js";
import type { Transport } from "../http/transport.js";
import type { GetCandlesResponse } from "../types/index.js";
import { CandleInterval, getCandles } from "./candles.js";

function stubTransport(response: unknown) {
  const request = vi.fn().mockResolvedValue(response);
  return { transport: { request } as unknown as Transport, request };
}

const MARKET = "ETH-USDT";
const NOW = 1_700_000_000;

// ---- getCandles ----

describe("getCandles", () => {
  it("calls the correct path with query params", async () => {
    const { transport, request } = stubTransport({
      interval: 60,
      candles: [],
    });
    await getCandles(transport, MARKET, {
      interval: CandleInterval.OneMinute,
      from: NOW - 3600,
      to: NOW,
    });
    expect(request).toHaveBeenCalledWith(
      "GET",
      "/api/v1/markets/ETH-USDT/candles",
      expect.objectContaining({
        query: { interval: 60, from: NOW - 3600, to: NOW },
      }),
    );
  });

  it("parses an empty candles array", async () => {
    const { transport } = stubTransport({ interval: 60, candles: [] });
    const result = await getCandles(transport, MARKET, {
      interval: CandleInterval.OneMinute,
      from: NOW - 60,
      to: NOW,
    });
    expect(result).toEqual<GetCandlesResponse>({ interval: 60, candles: [] });
  });

  it("parses candle OHLCV amounts as bigint from decimal strings", async () => {
    const { transport } = stubTransport({
      interval: 300,
      candles: [
        {
          bucket_start: NOW - 300,
          open: "1000",
          high: "1200",
          low: "950",
          close: "1100",
          volume: "99999999999999999",
        },
      ],
    });
    const result = await getCandles(transport, MARKET, {
      interval: CandleInterval.FiveMinutes,
      from: NOW - 300,
      to: NOW,
    });
    expect(result.candles[0]).toEqual({
      bucketStart: NOW - 300,
      open: 1000n,
      high: 1200n,
      low: 950n,
      close: 1100n,
      volume: 99999999999999999n,
    });
  });

  it("parses multiple candles in order", async () => {
    const candles = [
      { bucket_start: NOW - 120, open: "1", high: "2", low: "1", close: "2", volume: "10" },
      { bucket_start: NOW - 60,  open: "2", high: "3", low: "2", close: "3", volume: "20" },
    ];
    const { transport } = stubTransport({ interval: 60, candles });
    const result = await getCandles(transport, MARKET, {
      interval: CandleInterval.OneMinute,
      from: NOW - 120,
      to: NOW,
    });
    expect(result.candles).toHaveLength(2);
    expect(result.candles[0]?.bucketStart).toBe(NOW - 120);
    expect(result.candles[1]?.bucketStart).toBe(NOW - 60);
  });

  it("throws ValidationError for an empty market string", async () => {
    const { transport } = stubTransport({});
    await expect(
      getCandles(transport, "", { interval: 60, from: NOW - 60, to: NOW }),
    ).rejects.toBeInstanceOf(ValidationError);
  });

  it("throws ValidationError for an invalid interval", async () => {
    const { transport } = stubTransport({});
    await expect(
      getCandles(transport, MARKET, { interval: 45 as 60, from: NOW - 60, to: NOW }),
    ).rejects.toBeInstanceOf(ValidationError);
  });

  it("throws ValidationError when from >= to", async () => {
    const { transport } = stubTransport({});
    await expect(
      getCandles(transport, MARKET, { interval: 60, from: NOW, to: NOW }),
    ).rejects.toBeInstanceOf(ValidationError);
  });

  it("throws ValidationError when from > to", async () => {
    const { transport } = stubTransport({});
    await expect(
      getCandles(transport, MARKET, { interval: 60, from: NOW, to: NOW - 60 }),
    ).rejects.toBeInstanceOf(ValidationError);
  });

  it("throws ValidationError when range exceeds 1000 candles", async () => {
    const { transport } = stubTransport({});
    await expect(
      getCandles(transport, MARKET, {
        interval: CandleInterval.OneMinute,
        from: NOW - 60 * 1001,
        to: NOW,
      }),
    ).rejects.toBeInstanceOf(ValidationError);
  });

  it("does not throw at the exact 1000-candle boundary", async () => {
    const { transport } = stubTransport({ interval: 60, candles: [] });
    await expect(
      getCandles(transport, MARKET, {
        interval: CandleInterval.OneMinute,
        from: NOW - 60 * 1000,
        to: NOW,
      }),
    ).resolves.toBeDefined();
  });

  it("throws ParseError when the response shape is wrong", async () => {
    const { transport } = stubTransport("not an object");
    await expect(
      getCandles(transport, MARKET, { interval: 60, from: NOW - 60, to: NOW }),
    ).rejects.toBeInstanceOf(ParseError);
  });

  it("throws ParseError when a candle is missing a field", async () => {
    const { transport } = stubTransport({
      interval: 60,
      candles: [{ bucket_start: NOW }], // missing open/high/low/close/volume
    });
    await expect(
      getCandles(transport, MARKET, { interval: 60, from: NOW - 60, to: NOW }),
    ).rejects.toBeInstanceOf(ParseError);
  });
});
