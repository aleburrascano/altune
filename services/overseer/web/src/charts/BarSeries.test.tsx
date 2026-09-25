import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import type uPlot from "uplot";
import { BarSeries } from "./BarSeries";
import type { SeriesPoint } from "../types";

const plots = vi.hoisted(() => [] as { opts: uPlot.Options; data: uPlot.AlignedData }[]);

vi.mock("uplot", () => {
  const bars = vi.fn(() => () => ({}) as unknown);
  const Fake = function FakePlot(this: unknown, opts: uPlot.Options, data: uPlot.AlignedData) {
    plots.push({ opts, data });
    return {
      setSize: () => {},
      setData: () => {},
      destroy: () => {},
    };
  } as unknown as { paths: { bars: typeof bars } } & typeof Function;
  Fake.paths = { bars };
  return { default: Fake };
});

beforeEach(() => {
  plots.length = 0;
});

const seriesA: SeriesPoint[] = [
  { at: "2026-09-01T12:00:00Z", v: 3 },
  { at: "2026-09-01T12:00:30Z", v: 5 },
];

const seriesB: SeriesPoint[] = [
  { at: "2026-09-01T12:00:00Z", v: 1 },
  { at: "2026-09-01T12:00:30Z", v: 2 },
];

describe("BarSeries", () => {
  it("renders an empty state and draws nothing without points", async () => {
    render(<BarSeries series={[{ name: "Reads", points: [] }]} />);

    expect(screen.getByText(/no history for this range yet/)).toBeInTheDocument();
    await Promise.resolve();
    expect(plots).toHaveLength(0);
  });

  it("does not throw with a single point", async () => {
    render(<BarSeries series={[{ name: "Reads", points: [seriesA[0]] }]} />);

    await waitFor(() => expect(plots).toHaveLength(1));
  });

  it("stacks series into cumulative totals when stacked", async () => {
    render(
      <BarSeries
        series={[
          { name: "Reads", points: seriesA },
          { name: "Writes", points: seriesB },
        ]}
        stacked
      />,
    );

    await waitFor(() => expect(plots).toHaveLength(1));
    expect(plots[0].data[1]).toEqual([3, 5]);
    expect(plots[0].data[2]).toEqual([4, 7]);
  });
});
