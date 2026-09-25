import { describe, it, expect, vi, beforeEach } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import type uPlot from "uplot";
import { TimeSeries, toColumns } from "./TimeSeries";
import type { SeriesPoint } from "../types";

const plots = vi.hoisted(() => [] as { opts: uPlot.Options; data: uPlot.AlignedData; destroyed: boolean }[]);

vi.mock("uplot", () => ({
  default: function FakePlot(this: unknown, opts: uPlot.Options, data: uPlot.AlignedData) {
    const plot = { opts, data, destroyed: false };
    plots.push(plot);
    return {
      setSize: () => {},
      destroy: () => {
        plot.destroyed = true;
      },
    };
  },
}));

beforeEach(() => {
  plots.length = 0;
});

const points: SeriesPoint[] = [
  { at: "2026-09-01T12:00:00Z", v: 30 },
  { at: "2026-09-01T12:00:30Z", v: 45 },
  { at: "2026-09-01T12:01:00Z", v: 61 },
];

const ms = (v: number) => `${v} ms`;

function hoverAt(idx: number | null) {
  const plot = plots[plots.length - 1];
  const setCursor = plot.opts.hooks?.setCursor;
  const hook = Array.isArray(setCursor) ? setCursor[0] : setCursor;
  act(() => hook?.({ cursor: { idx } } as unknown as uPlot));
}

describe("TimeSeries", () => {
  it("converts points to uPlot columns of unix seconds and values", () => {
    expect(toColumns(points)).toEqual([
      [Date.parse("2026-09-01T12:00:00Z") / 1000, Date.parse("2026-09-01T12:00:30Z") / 1000, Date.parse("2026-09-01T12:01:00Z") / 1000],
      [30, 45, 61],
    ]);
  });

  it("draws the points and reads out the latest value by default", async () => {
    render(<TimeSeries title="Latency" kind="line" colorToken="--color-accent" points={points} formatValue={ms} />);

    await waitFor(() => expect(plots).toHaveLength(1));
    expect(plots[0].data[1]).toEqual([30, 45, 61]);
    expect(screen.getByRole("figure", { name: "Latency" })).toHaveTextContent("61 ms");
  });

  it("reads out the hovered point and falls back to the latest when the cursor leaves", async () => {
    render(<TimeSeries title="Latency" kind="line" colorToken="--color-accent" points={points} formatValue={ms} />);
    await waitFor(() => expect(plots).toHaveLength(1));
    const figure = screen.getByRole("figure", { name: "Latency" });

    hoverAt(0);
    expect(figure).toHaveTextContent("30 ms");
    expect(figure).not.toHaveTextContent("61 ms");

    hoverAt(null);
    expect(figure).toHaveTextContent("61 ms");
  });

  it("fills an area series and leaves a line series unfilled", async () => {
    const { rerender } = render(
      <TimeSeries title="Uptime" kind="area" colorToken="--color-ok" points={points} formatValue={ms} />,
    );
    await waitFor(() => expect(plots).toHaveLength(1));
    expect(plots[0].opts.series[1].fill).toBeTruthy();

    rerender(<TimeSeries title="Uptime" kind="line" colorToken="--color-ok" points={points} formatValue={ms} />);
    await waitFor(() => expect(plots).toHaveLength(2));
    expect(plots[1].opts.series[1].fill).toBeUndefined();
    expect(plots[0].destroyed).toBe(true);
  });

  it("shows an empty notice and draws nothing without points", async () => {
    render(<TimeSeries title="Uptime" kind="area" colorToken="--color-ok" points={[]} formatValue={ms} />);

    expect(screen.getByText(/no history for this range yet/)).toBeInTheDocument();
    await Promise.resolve();
    expect(plots).toHaveLength(0);
  });

  it("does not rebuild the chart when the parent re-renders with the same points", async () => {
    const { rerender } = render(
      <TimeSeries title="Latency" kind="line" colorToken="--color-accent" points={points} formatValue={ms} />,
    );
    await waitFor(() => expect(plots).toHaveLength(1));

    rerender(<TimeSeries title="Latency" kind="line" colorToken="--color-accent" points={points} formatValue={(v) => `${v}ms`} />);
    await Promise.resolve();

    expect(plots).toHaveLength(1);
    expect(screen.getByRole("figure", { name: "Latency" })).toHaveTextContent("61ms");
  });

  it("destroys the chart on unmount", async () => {
    const { unmount } = render(
      <TimeSeries title="Latency" kind="line" colorToken="--color-accent" points={points} formatValue={ms} />,
    );
    await waitFor(() => expect(plots).toHaveLength(1));

    unmount();

    expect(plots[0].destroyed).toBe(true);
  });
});
