import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor } from "@testing-library/react";
import type uPlot from "uplot";
import { BarSeries } from "./BarSeries";
import type { SeriesPoint } from "../types";

const plots = vi.hoisted(() => [] as { data: uPlot.AlignedData; updates: uPlot.AlignedData[]; destroyed: boolean }[]);

vi.mock("uplot", () => {
  const bars = vi.fn(() => () => ({}) as unknown);
  const Fake = function FakePlot(this: unknown, _opts: uPlot.Options, data: uPlot.AlignedData) {
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
  } as unknown as { paths: { bars: typeof bars } } & typeof Function;
  Fake.paths = { bars };
  return { default: Fake };
});

beforeEach(() => {
  plots.length = 0;
});

const firstPoll: SeriesPoint[] = [
  { at: "2026-09-01T12:00:00Z", v: 3 },
  { at: "2026-09-01T12:00:30Z", v: 5 },
];

const nextPoll: SeriesPoint[] = [...firstPoll, { at: "2026-09-01T12:01:00Z", v: 8 }];

describe("BarSeries across polls", () => {
  it("updates the existing chart's data instead of rebuilding it", async () => {
    const { rerender } = render(<BarSeries series={[{ name: "Reads", points: firstPoll }]} />);
    await waitFor(() => expect(plots).toHaveLength(1));

    rerender(<BarSeries series={[{ name: "Reads", points: nextPoll }]} />);

    expect(plots).toHaveLength(1);
    expect(plots[0].destroyed).toBe(false);
    expect(plots[0].updates.at(-1)?.[1]).toEqual([3, 5, 8]);
  });
});
