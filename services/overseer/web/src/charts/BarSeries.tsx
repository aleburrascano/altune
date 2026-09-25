import { useEffect, useMemo, useRef, useState } from "react";
import type uPlot from "uplot";
import type { SeriesPoint } from "../types";
import { FALLBACK_WIDTH, alignToTimestamps, gridAxes, loadUPlot, seriesColor, unionTimestamps, watchResize } from "./uplot";

export interface BarSeriesItem {
  name: string;
  points: SeriesPoint[];
}

export interface BarSeriesProps {
  series: BarSeriesItem[];
  stacked?: boolean;
}

const HEIGHT = 140;

function stackColumns(columns: (number | null)[][]): (number | null)[][] {
  const running = columns[0]?.map(() => 0) ?? [];
  return columns.map((col) =>
    col.map((v, i) => {
      running[i] += v ?? 0;
      return running[i];
    }),
  );
}

function barsOptions(
  UPlot: typeof uPlot,
  series: BarSeriesItem[],
  stacked: boolean,
  width: number,
): uPlot.Options {
  const bars = UPlot.paths.bars?.({ size: [stacked ? 0.9 : 0.6] });
  return {
    width,
    height: HEIGHT,
    legend: { show: false },
    cursor: { show: false },
    scales: { x: { time: true }, y: { range: (_u, min, max) => [Math.min(0, min), max] } },
    axes: gridAxes(),
    series: [
      {},
      ...series.map((s, i) => ({
        label: s.name,
        stroke: seriesColor(i),
        fill: seriesColor(i),
        paths: bars,
        points: { show: false },
      })),
    ],
  };
}

export function BarSeries(props: BarSeriesProps) {
  const host = useRef<HTMLDivElement>(null);
  const plotRef = useRef<uPlot | undefined>(undefined);
  const latestProps = useRef(props);
  latestProps.current = props;
  const [chartFailed, setChartFailed] = useState(false);
  const { series, stacked = false } = props;
  const xs = useMemo(() => unionTimestamps(series), [series]);
  const columns = useMemo(() => {
    const raw = series.map((s) => alignToTimestamps(s.points, xs));
    return stacked ? stackColumns(raw) : raw;
  }, [series, xs, stacked]);
  const hasPoints = xs.length > 0;

  useEffect(() => {
    const el = host.current;
    if (!el || !hasPoints) return;
    let plot: uPlot | undefined;
    let disposed = false;
    loadUPlot().then(
      (UPlot) => {
        if (disposed) return;
        const options = barsOptions(UPlot, latestProps.current.series, stacked, el.clientWidth || FALLBACK_WIDTH);
        const latestXs = unionTimestamps(latestProps.current.series);
        const latestRaw = latestProps.current.series.map((s) => alignToTimestamps(s.points, latestXs));
        const latestColumns = stacked ? stackColumns(latestRaw) : latestRaw;
        plot = new UPlot(options, [latestXs, ...latestColumns] as uPlot.AlignedData, el);
        plotRef.current = plot;
      },
      () => {
        if (!disposed) setChartFailed(true);
      },
    );
    const fit = () => plot?.setSize({ width: el.clientWidth || FALLBACK_WIDTH, height: HEIGHT });
    const stopWatching = watchResize(el, fit);
    return () => {
      disposed = true;
      stopWatching();
      plot?.destroy();
      plotRef.current = undefined;
    };
  }, [hasPoints, stacked]);

  useEffect(() => {
    plotRef.current?.setData([xs, ...columns] as uPlot.AlignedData);
  }, [xs, columns]);

  const label = series.map((s) => s.name).join(", ");

  return (
    <figure className="m-0 flex min-w-0 flex-col gap-1" aria-label={label}>
      {xs.length === 0 && <p className="empty">no history for this range yet</p>}
      {chartFailed && <p className="empty">chart could not load</p>}
      {xs.length > 0 && !chartFailed && <div ref={host} className="w-full min-w-0" />}
    </figure>
  );
}
