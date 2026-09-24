import { useEffect, useRef, useState } from "react";
import type uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import type { SeriesPoint } from "../types";

export type TimeSeriesKind = "line" | "area";

export interface TimeSeriesProps {
  title: string;
  points: SeriesPoint[];
  kind: TimeSeriesKind;
  colorToken: string;
  formatValue: (v: number) => string;
  valueRange?: [number, number];
}

const CHART_HEIGHT = 140;
const FALLBACK_WIDTH = 480;
const FALLBACK_COLOR = "#5b8cff";

let uPlotLoad: Promise<typeof uPlot> | undefined;

function loadUPlot(): Promise<typeof uPlot> {
  const pending =
    uPlotLoad ??
    import("uplot").then(
      (mod) => mod.default,
      (err: unknown) => {
        uPlotLoad = undefined;
        throw err;
      },
    );
  uPlotLoad = pending;
  return pending;
}

export function toColumns(points: SeriesPoint[]): uPlot.AlignedData {
  return [points.map((p) => Date.parse(p.at) / 1000), points.map((p) => p.v)];
}

function resolveToken(token: string): string {
  const resolved = getComputedStyle(document.documentElement).getPropertyValue(token).trim();
  return resolved || FALLBACK_COLOR;
}

function translucent(color: string): string {
  return /^#[0-9a-f]{6}$/i.test(color) ? `${color}33` : color;
}

function chartOptions(
  props: TimeSeriesProps,
  width: number,
  onHover: (idx: number | null) => void,
  formatValue: (v: number) => string,
): uPlot.Options {
  const stroke = resolveToken(props.colorToken);
  const grid = { stroke: resolveToken("--color-border"), width: 1 };
  const axisColor = resolveToken("--color-fg-faint");
  return {
    width,
    height: CHART_HEIGHT,
    legend: { show: false },
    cursor: { y: false },
    scales: { x: { time: true }, y: props.valueRange ? { range: props.valueRange } : {} },
    axes: [
      { stroke: axisColor, grid, ticks: grid },
      { stroke: axisColor, grid, ticks: grid, values: (_u, vals) => vals.map(formatValue) },
    ],
    series: [
      {},
      {
        label: props.title,
        stroke,
        width: 1.5,
        fill: props.kind === "area" ? translucent(stroke) : undefined,
        points: { show: false },
      },
    ],
    hooks: { setCursor: [(u) => onHover(u.cursor.idx ?? null)] },
  };
}

function formatAt(at: string): string {
  return new Date(at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

export function TimeSeries(props: TimeSeriesProps) {
  const host = useRef<HTMLDivElement>(null);
  const plotRef = useRef<uPlot | undefined>(undefined);
  const latestProps = useRef(props);
  latestProps.current = props;
  const [hovered, setHovered] = useState<number | null>(null);
  const [chartFailed, setChartFailed] = useState(false);
  const { points, kind, colorToken, title } = props;
  const [rangeLow, rangeHigh] = props.valueRange ?? [];
  const hasPoints = points.length > 0;

  useEffect(() => {
    const el = host.current;
    if (!el || !hasPoints) return;
    let plot: uPlot | undefined;
    let disposed = false;
    const options = chartOptions(latestProps.current, el.clientWidth || FALLBACK_WIDTH, setHovered, (v) =>
      latestProps.current.formatValue(v),
    );
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
    const fit = () => plot?.setSize({ width: el.clientWidth || FALLBACK_WIDTH, height: CHART_HEIGHT });
    window.addEventListener("resize", fit);
    return () => {
      disposed = true;
      window.removeEventListener("resize", fit);
      plot?.destroy();
      plotRef.current = undefined;
    };
  }, [hasPoints, kind, colorToken, title, rangeLow, rangeHigh]);

  useEffect(() => {
    plotRef.current?.setData(toColumns(points));
  }, [points]);

  const shown = (hovered === null ? undefined : points[hovered]) ?? points[points.length - 1];

  return (
    <figure className="m-0 flex min-w-0 flex-col gap-1" aria-label={props.title}>
      <figcaption className="flex items-baseline justify-between gap-2 text-sm text-fg-dim">
        <span>{props.title}</span>
        {shown && (
          <span className="font-mono text-xs text-fg">
            {props.formatValue(shown.v)} · {formatAt(shown.at)}
          </span>
        )}
      </figcaption>
      {points.length === 0 && <p className="empty">no history for this range yet</p>}
      {chartFailed && <p className="empty">chart could not load</p>}
      {points.length > 0 && !chartFailed && (
        <div ref={host} className="w-full min-w-0" />
      )}
    </figure>
  );
}
