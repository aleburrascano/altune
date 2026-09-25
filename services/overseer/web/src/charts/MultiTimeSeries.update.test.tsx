import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import type uPlot from "uplot";
import { MultiTimeSeries } from "./MultiTimeSeries";
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

const upFirstPoll: SeriesPoint[] = [
  { at: "2026-09-01T12:00:00Z", v: 1 },
  { at: "2026-09-01T12:00:30Z", v: 1 },
];

const upNextPoll: SeriesPoint[] = [...upFirstPoll, { at: "2026-09-01T12:01:00Z", v: 1 }];

const latencyFirstPoll: SeriesPoint[] = [
  { at: "2026-09-01T12:00:00Z", v: 30 },
  { at: "2026-09-01T12:00:30Z", v: 45 },
];

const latencyNextPoll: SeriesPoint[] = [...latencyFirstPoll, { at: "2026-09-01T12:01:00Z", v: 61 }];

describe("MultiTimeSeries across polls", () => {
  it("updates the existing chart's data instead of rebuilding it", async () => {
    const { rerender } = render(
      <MultiTimeSeries
        series={[
          { name: "Uptime", points: upFirstPoll },
          { name: "Latency", points: latencyFirstPoll, unit: "ms" },
        ]}
        range="1h"
      />,
    );
    await waitFor(() => expect(plots).toHaveLength(1));

    rerender(
      <MultiTimeSeries
        series={[
          { name: "Uptime", points: upNextPoll },
          { name: "Latency", points: latencyNextPoll, unit: "ms" },
        ]}
        range="1h"
      />,
    );

    expect(plots).toHaveLength(1);
    expect(plots[0].destroyed).toBe(false);
    expect(plots[0].updates.at(-1)?.[2]).toEqual([30, 45, 61]);
    expect(screen.getByRole("figure", { name: "Uptime, Latency" })).toHaveTextContent("Latency 61 ms");
  });
});
