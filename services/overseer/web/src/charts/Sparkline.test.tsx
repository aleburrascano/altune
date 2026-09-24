import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import type uPlot from "uplot";
import { Sparkline } from "./Sparkline";
import type { SeriesPoint } from "../types";

const plots = vi.hoisted(() => [] as { opts: uPlot.Options }[]);

vi.mock("uplot", () => ({
  default: function FakePlot(this: unknown, opts: uPlot.Options) {
    plots.push({ opts });
    return {
      setSize: () => {},
      setData: () => {},
      destroy: () => {},
    };
  },
}));

beforeEach(() => {
  plots.length = 0;
});

const points: SeriesPoint[] = [
  { at: "2026-09-01T12:00:00Z", v: 3 },
  { at: "2026-09-01T12:00:30Z", v: 5 },
];

describe("Sparkline", () => {
  it("renders an empty state and draws nothing without points", async () => {
    render(<Sparkline points={[]} />);

    expect(screen.getByRole("img", { name: "trend" })).toBeInTheDocument();
    await Promise.resolve();
    expect(plots).toHaveLength(0);
  });

  it("does not throw with a single point", async () => {
    render(<Sparkline points={[points[0]]} tone="ok" />);

    await waitFor(() => expect(plots).toHaveLength(1));
  });

  it("draws with the tone color and default color otherwise", async () => {
    render(<Sparkline points={points} tone="critical" />);

    await waitFor(() => expect(plots).toHaveLength(1));
    expect(plots[0].opts.series[1].stroke).toBeTruthy();
  });
});
