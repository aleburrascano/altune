import { describe, it, expect, vi, beforeEach } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import type uPlot from "uplot";
import { MultiTimeSeries } from "./MultiTimeSeries";
import type { SeriesPoint } from "../types";

const plots = vi.hoisted(() => [] as { opts: uPlot.Options; data: uPlot.AlignedData; destroyed: boolean }[]);

vi.mock("uplot", () => ({
  default: function FakePlot(this: unknown, opts: uPlot.Options, data: uPlot.AlignedData) {
    const plot = { opts, data, destroyed: false };
    plots.push(plot);
    return {
      setSize: () => {},
      setData: () => {},
      destroy: () => {
        plot.destroyed = true;
      },
    };
  },
}));

beforeEach(() => {
  plots.length = 0;
});

const upPoints: SeriesPoint[] = [
  { at: "2026-09-01T12:00:00Z", v: 1 },
  { at: "2026-09-01T12:00:30Z", v: 1 },
];

const latencyPoints: SeriesPoint[] = [
  { at: "2026-09-01T12:00:00Z", v: 30 },
  { at: "2026-09-01T12:00:30Z", v: 45 },
];

function hoverAt(idx: number | null) {
  const plot = plots[plots.length - 1];
  const setCursor = plot.opts.hooks?.setCursor;
  const hook = Array.isArray(setCursor) ? setCursor[0] : setCursor;
  act(() => hook?.({ cursor: { idx } } as unknown as uPlot));
}

describe("MultiTimeSeries", () => {
  it("renders an empty state and draws nothing without points", async () => {
    render(<MultiTimeSeries series={[{ name: "Uptime", points: [] }]} range="1h" />);

    expect(screen.getByText(/no history for this range yet/)).toBeInTheDocument();
    await Promise.resolve();
    expect(plots).toHaveLength(0);
  });

  it("does not throw with a single point", async () => {
    render(<MultiTimeSeries series={[{ name: "Uptime", points: [upPoints[0]] }]} range="1h" />);

    await waitFor(() => expect(plots).toHaveLength(1));
  });

  it("draws two series and reads out both values and the time on hover", async () => {
    render(
      <MultiTimeSeries
        series={[
          { name: "Uptime", points: upPoints },
          { name: "Latency", points: latencyPoints, unit: "ms" },
        ]}
        range="1h"
      />,
    );

    await waitFor(() => expect(plots).toHaveLength(1));
    const figure = screen.getByRole("figure", { name: "Uptime, Latency" });

    const expectedTime = new Date(upPoints[0].at).toLocaleTimeString([], {
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });

    hoverAt(0);
    expect(figure).toHaveTextContent("Uptime 1");
    expect(figure).toHaveTextContent("Latency 30 ms");
    expect(figure).toHaveTextContent(expectedTime);
  });
});
