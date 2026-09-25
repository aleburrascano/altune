import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import type uPlot from "uplot";
import { TimeSeries } from "./TimeSeries";
import type { SeriesPoint } from "../types";

const plots = vi.hoisted(() => [] as { data: uPlot.AlignedData; updates: uPlot.AlignedData[]; destroyed: boolean }[]);

vi.mock("uplot", () => ({
  default: function FakePlot(this: unknown, _opts: uPlot.Options, data: uPlot.AlignedData) {
    const plot = { data, updates: [] as uPlot.AlignedData[], destroyed: false };
    plots.push(plot);
    return {
      setSize: () => {},
      setData: (next: uPlot.AlignedData) => {
        plot.updates.push(next);
      },
      destroy: () => {
        plot.destroyed = true;
      },
    };
  },
}));

beforeEach(() => {
  plots.length = 0;
});

const ms = (v: number) => `${v} ms`;

const firstPoll: SeriesPoint[] = [
  { at: "2026-09-01T12:00:00Z", v: 30 },
  { at: "2026-09-01T12:00:30Z", v: 45 },
];

const nextPoll: SeriesPoint[] = [...firstPoll, { at: "2026-09-01T12:01:00Z", v: 61 }];

describe("TimeSeries across polls", () => {
  it("updates the existing chart's data instead of rebuilding it", async () => {
    const { rerender } = render(
      <TimeSeries title="Latency" kind="line" colorToken="--color-accent" points={firstPoll} formatValue={ms} />,
    );
    await waitFor(() => expect(plots).toHaveLength(1));

    rerender(<TimeSeries title="Latency" kind="line" colorToken="--color-accent" points={nextPoll} formatValue={ms} />);

    expect(plots).toHaveLength(1);
    expect(plots[0].destroyed).toBe(false);
    expect(plots[0].updates.at(-1)?.[1]).toEqual([30, 45, 61]);
    expect(screen.getByRole("figure", { name: "Latency" })).toHaveTextContent("61 ms");
  });

  it("draws the newest points when they arrive while the chart is still loading", async () => {
    const { rerender } = render(
      <TimeSeries title="Latency" kind="line" colorToken="--color-accent" points={firstPoll} formatValue={ms} />,
    );
    rerender(<TimeSeries title="Latency" kind="line" colorToken="--color-accent" points={nextPoll} formatValue={ms} />);

    await waitFor(() => expect(plots).toHaveLength(1));
    expect(plots[0].data[1]).toEqual([30, 45, 61]);
  });
});
