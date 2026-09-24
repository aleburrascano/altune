import { useEffect, useMemo, useRef, useState } from "react";
import type uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import type { Range, SeriesPoint } from "../types";
import type { TimeSeriesKind } from "./TimeSeries";
import {
  FALLBACK_WIDTH,
  alignToTimestamps,
  formatTime,
  gridAxes,
  loadUPlot,
  seriesColor,
  translucent,
  unionTimestamps,
  watchResize,
} from "./uplot";

export interface MultiSeries {
  name: string;
  points: SeriesPoint[];
  unit?: string;
}

export interface MultiTimeSeriesProps {
  series: MultiSeries[];
  range: Range;
  kind?: TimeSeriesKind;
  height?: number;
}

const DEFAULT_HEIGHT = 140;

function toMultiColumns(series: MultiSeries[], xs: number[]): (number | null)[][] {
  return series.map((s) => alignToTimestamps(s.points, xs));
}

function multiChartOptions(
  series: MultiSeries[],
  kind: TimeSeriesKind,
  height: number,
  width: number,
  onHover: (idx: number | null) => void,
): uPlot.Options {
  return {
    width,
    height,
    legend: { show: false },
    cursor: { y: false },
    scales: { x: { time: true } },
    axes: gridAxes(),
    series: [
      {},
      ...series.map((s, i) => {
        const stroke = seriesColor(i);
        return {
          label: s.name,
          stroke,
          width: 1.5,
          fill: kind === "area" ? translucent(stroke) : undefined,
          points: { show: false },
        };
      }),
    ],
    hooks: { setCursor: [(u) => onHover(u.cursor.idx ?? null)] },
  };
}

function formatMultiValue(v: number | null, unit?: string): string {
  if (v === null) return "—";
  return unit ? `${v} ${unit}` : `${v}`;
}

export function MultiTimeSeries(props: MultiTimeSeriesProps) {
  const host = useRef<HTMLDivElement>(null);
  const plotRef = useRef<uPlot | undefined>(undefined);
  const latestProps = useRef(props);
  latestProps.current = props;
  const [hovered, setHovered] = useState<number | null>(null);
  const [chartFailed, setChartFailed] = useState(false);
  const { series, kind = "line", height = DEFAULT_HEIGHT, range } = props;
  const xs = useMemo(() => unionTimestamps(series), [series]);
  const columns = useMemo(() => toMultiColumns(series, xs), [series, xs]);
  const hasPoints = xs.length > 0;

  useEffect(() => {
    const el = host.current;
    if (!el || !hasPoints) return;
    let plot: uPlot | undefined;
    let disposed = false;
    const options = multiChartOptions(latestProps.current.series, kind, height, el.clientWidth || FALLBACK_WIDTH, setHovered);
    loadUPlot().then(
      (UPlot) => {
        if (disposed) return;
        const latestXs = unionTimestamps(latestProps.current.series);
        plot = new UPlot(options, [latestXs, ...toMultiColumns(latestProps.current.series, latestXs)] as uPlot.AlignedData, el);
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
  }, [hasPoints, kind, height]);

  useEffect(() => {
    plotRef.current?.setData([xs, ...columns] as uPlot.AlignedData);
  }, [xs, columns]);

  const shownIdx = hovered ?? xs.length - 1;
  const shownAt = xs[shownIdx];
  const label = series.map((s) => s.name).join(", ");

  return (
    <figure className="m-0 flex min-w-0 flex-col gap-1" aria-label={label}>
      <figcaption className="flex items-baseline justify-between gap-2 text-sm text-fg-dim">
        <span>{label}</span>
        {shownAt !== undefined && (
          <span className="font-mono text-xs text-fg">
            {formatTime(shownAt * 1000, range === "7d")}
            {series.map((s, i) => ` · ${s.name} ${formatMultiValue(columns[i][shownIdx], s.unit)}`)}
          </span>
        )}
      </figcaption>
      {xs.length === 0 && <p className="empty">no history for this range yet</p>}
      {chartFailed && <p className="empty">chart could not load</p>}
      {xs.length > 0 && !chartFailed && <div ref={host} className="w-full min-w-0" />}
    </figure>
  );
}
