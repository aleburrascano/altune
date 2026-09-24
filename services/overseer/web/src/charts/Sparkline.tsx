import { useEffect, useRef, useState } from "react";
import type uPlot from "uplot";
import type { SeriesPoint } from "../types";
import { FALLBACK_WIDTH, loadUPlot, resolveToken, translucent, watchResize } from "./uplot";

export type SparklineTone = "ok" | "warn" | "critical";

export interface SparklineProps {
  points: SeriesPoint[];
  tone?: SparklineTone;
  height?: number;
}

const DEFAULT_HEIGHT = 32;

function toneToken(tone?: SparklineTone): string {
  if (tone === "ok") return "--color-ok";
  if (tone === "warn") return "--color-warn";
  if (tone === "critical") return "--color-critical";
  return "--color-accent";
}

function sparklineOptions(tone: SparklineTone | undefined, height: number, width: number): uPlot.Options {
  const stroke = resolveToken(toneToken(tone));
  return {
    width,
    height,
    legend: { show: false },
    cursor: { show: false },
    scales: { x: { time: true }, y: {} },
    axes: [{ show: false }, { show: false }],
    series: [
      {},
      {
        stroke,
        width: 1.5,
        fill: translucent(stroke),
        points: { show: false },
      },
    ],
  };
}

function toColumns(points: SeriesPoint[]): uPlot.AlignedData {
  return [points.map((p) => Date.parse(p.at) / 1000), points.map((p) => p.v)];
}

export function Sparkline(props: SparklineProps) {
  const host = useRef<HTMLDivElement>(null);
  const plotRef = useRef<uPlot | undefined>(undefined);
  const latestProps = useRef(props);
  latestProps.current = props;
  const [chartFailed, setChartFailed] = useState(false);
  const { points, tone, height = DEFAULT_HEIGHT } = props;
  const hasPoints = points.length > 0;

  useEffect(() => {
    const el = host.current;
    if (!el || !hasPoints) return;
    let plot: uPlot | undefined;
    let disposed = false;
    const options = sparklineOptions(tone, height, el.clientWidth || FALLBACK_WIDTH);
    loadUPlot().then(
      (UPlot) => {
        if (disposed) return;
        plot = new UPlot(options, toColumns(latestProps.current.points), el);
        plotRef.current = plot;
      },
      () => {
        if (!disposed) setChartFailed(true);
      },
    );
    const fit = () => plot?.setSize({ width: el.clientWidth || FALLBACK_WIDTH, height });
    const stopWatching = watchResize(el, fit);
    return () => {
      disposed = true;
      stopWatching();
      plot?.destroy();
      plotRef.current = undefined;
    };
  }, [hasPoints, tone, height]);

  useEffect(() => {
    plotRef.current?.setData(toColumns(points));
  }, [points]);

  return (
    <div className="min-w-0" role="img" aria-label="trend">
      {points.length === 0 && <span className="block w-full border-t border-dashed border-border" />}
      {chartFailed && <span className="sr-only">trend unavailable</span>}
      {points.length > 0 && !chartFailed && <div ref={host} className="w-full" />}
    </div>
  );
}
