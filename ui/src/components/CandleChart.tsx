import { useEffect, useRef, useState } from "react";
import {
  createChart,
  type IChartApi,
  type ISeriesApi,
  type CandlestickData,
  type UTCTimestamp,
} from "lightweight-charts";
import type { MatchingEngineClient } from "ts-sdk";
import { CandleInterval } from "ts-sdk";
import { useCandleStream } from "../hooks/useCandleStream.ts";
import type { OHLCBar } from "../hooks/useCandleStream.ts";
import { Skeleton } from "./Skeleton.tsx";

// ── Interval selector ─────────────────────────────────────────────────────

const INTERVALS: { label: string; value: number }[] = [
  { label: "1m",  value: CandleInterval.OneMinute },
  { label: "5m",  value: CandleInterval.FiveMinutes },
  { label: "15m", value: CandleInterval.FifteenMinutes },
  { label: "1h",  value: CandleInterval.OneHour },
  { label: "4h",  value: CandleInterval.FourHours },
  { label: "1d",  value: CandleInterval.OneDay },
];

// OHLCBar values are raw quantum units (see useCandleStream) — scale down
// by the market's quote decimals here, at display time, same as fmtUnits
// does for every other price shown in the UI.
function toChartBar(b: OHLCBar, quoteDecimals: number): CandlestickData {
  const scale = 10 ** quoteDecimals;
  return {
    time: b.time as UTCTimestamp,
    open: b.open / scale,
    high: b.high / scale,
    low: b.low / scale,
    close: b.close / scale,
  };
}

// ── Chart component ───────────────────────────────────────────────────────

interface Props {
  client: MatchingEngineClient;
  market: string;
  quoteDecimals: number;
}

export function CandleChart({ client, market, quoteDecimals }: Props) {
  const [interval, setInterval] = useState<number>(CandleInterval.OneMinute);
  const containerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const seriesRef = useRef<ISeriesApi<"Candlestick"> | null>(null);
  const prevBarsLen = useRef(0);

  const { bars, formingBar, status, error } = useCandleStream(client, market, interval);

  // Create / destroy the chart when the container mounts.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    const chart = createChart(el, {
      layout: {
        background: { color: "transparent" },
        textColor: "#8888aa",
      },
      grid: {
        vertLines: { color: "#1c1c28" },
        horzLines: { color: "#1c1c28" },
      },
      crosshair: {
        vertLine: { color: "#555568", width: 1, labelBackgroundColor: "#1a1a24" },
        horzLine: { color: "#555568", width: 1, labelBackgroundColor: "#1a1a24" },
      },
      rightPriceScale: { borderColor: "#252535" },
      timeScale: { borderColor: "#252535", timeVisible: true, secondsVisible: false },
      handleScroll: true,
      handleScale: true,
    });

    const series = chart.addCandlestickSeries({
      upColor: "#00c076",
      downColor: "#ff5353",
      borderUpColor: "#00c076",
      borderDownColor: "#ff5353",
      wickUpColor: "#00c076",
      wickDownColor: "#ff5353",
      priceFormat: {
        type: "price",
        precision: quoteDecimals,
        minMove: 1 / 10 ** quoteDecimals,
      },
    });

    chartRef.current = chart;
    seriesRef.current = series;

    const ro = new ResizeObserver(() => {
      chart.applyOptions({ width: el.clientWidth, height: el.clientHeight });
    });
    ro.observe(el);

    return () => {
      ro.disconnect();
      chart.remove();
      chartRef.current = null;
      seriesRef.current = null;
      prevBarsLen.current = 0;
    };
  }, []);

  // Keep the Y-axis precision in sync when switching to a market quoted in
  // a different asset (e.g. USDT with 6 decimals vs. BTC with 9).
  useEffect(() => {
    seriesRef.current?.applyOptions({
      priceFormat: {
        type: "price",
        precision: quoteDecimals,
        minMove: 1 / 10 ** quoteDecimals,
      },
    });
  }, [quoteDecimals]);

  // When the market or interval changes, clear the series immediately so
  // stale data from the previous interval (a different time granularity)
  // can never linger and violate lightweight-charts' monotonic-time
  // requirement once the new interval's live formingBar starts updating.
  useEffect(() => {
    seriesRef.current?.setData([]);
    prevBarsLen.current = 0;
  }, [market, interval]);

  // Push historical bars to the series (only when they arrive as a fresh set).
  useEffect(() => {
    const series = seriesRef.current;
    if (!series || bars.length === 0) return;

    if (bars.length !== prevBarsLen.current) {
      series.setData(bars.map((b) => toChartBar(b, quoteDecimals)));
      prevBarsLen.current = bars.length;
      chartRef.current?.timeScale().fitContent();
    }
  }, [bars, quoteDecimals]);

  // Update the forming (live) bar without resetting the whole series.
  useEffect(() => {
    const series = seriesRef.current;
    if (!series || !formingBar) return;
    series.update(toChartBar(formingBar, quoteDecimals));
  }, [formingBar, quoteDecimals]);

  return (
    <div style={s.wrapper}>
      {/* Toolbar */}
      <div style={s.toolbar}>
        <div style={s.intervalGroup}>
          {INTERVALS.map((iv) => (
            <button
              key={iv.value}
              onClick={() => setInterval(iv.value)}
              style={{
                ...s.intervalBtn,
                ...(interval === iv.value ? s.intervalBtnActive : {}),
              }}
            >
              {iv.label}
            </button>
          ))}
        </div>
        <StatusBadge status={status} />
      </div>

      {/* Chart area */}
      <div style={{ position: "relative", flex: 1, minHeight: 0 }}>
        <div ref={containerRef} style={s.chart} />

        {status === "loading" && bars.length === 0 && (
          <div style={s.overlay}>
            <Skeleton width="100%" height="100%" borderRadius={0} />
          </div>
        )}

        {status === "error" && (
          <div style={{ ...s.overlay, ...s.errorOverlay }}>
            {error}
          </div>
        )}
      </div>
    </div>
  );
}

function StatusBadge({ status }: { status: "loading" | "live" | "error" }) {
  const label =
    status === "live" ? "live" : status === "error" ? "error" : "loading…";
  const color =
    status === "live"
      ? "var(--green)"
      : status === "error"
        ? "var(--red)"
        : "var(--text-muted)";
  return (
    <span style={{ fontSize: 11, color, fontWeight: 600 }}>{label}</span>
  );
}

// ── Styles ────────────────────────────────────────────────────────────────

const s = {
  wrapper: {
    display: "flex",
    flexDirection: "column" as const,
    height: "100%",
    background: "var(--bg-panel)",
    borderBottom: "1px solid var(--border)",
    overflow: "hidden",
    animation: "fade-in 200ms ease",
  },
  toolbar: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 8,
    padding: "8px 12px",
    borderBottom: "1px solid var(--border-subtle)",
    flexShrink: 0,
  },
  intervalGroup: {
    display: "flex",
    gap: 2,
  },
  intervalBtn: {
    background: "none",
    color: "var(--text-secondary)",
    padding: "3px 8px",
    borderRadius: "var(--radius-sm)",
    fontSize: 12,
    fontWeight: 500,
  } as const,
  intervalBtnActive: {
    background: "var(--accent-dim)",
    color: "var(--accent)",
  } as const,
  chart: {
    position: "absolute" as const,
    inset: 0,
  },
  overlay: {
    position: "absolute" as const,
    inset: 0,
    pointerEvents: "none" as const,
  },
  errorOverlay: {
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    background: "var(--bg-panel)",
    color: "var(--red)",
    fontSize: 12,
    pointerEvents: "auto" as const,
  },
} as const;
