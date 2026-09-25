import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { UptimeStrip } from "./UptimeStrip";
import type { SeriesPoint } from "../types";

describe("UptimeStrip", () => {
  it("renders an empty state without points", () => {
    render(<UptimeStrip points={[]} />);

    expect(screen.getByText(/no uptime history for this range yet/)).toBeInTheDocument();
  });

  it("does not throw with a single point", () => {
    render(<UptimeStrip points={[{ at: "2026-09-01T12:00:00Z", v: 1 }]} />);

    expect(screen.getByLabelText("uptime strip")).toBeInTheDocument();
  });

  it("titles each bar with its up/degraded/down state", () => {
    const points: SeriesPoint[] = [
      { at: "2026-09-01T12:00:00Z", v: 1 },
      { at: "2026-09-01T12:00:30Z", v: 0.5 },
      { at: "2026-09-01T12:01:00Z", v: 0 },
    ];
    render(<UptimeStrip points={points} />);

    const strip = screen.getByLabelText("uptime strip");
    const bars = strip.querySelectorAll("span");
    expect(bars[0].title).toMatch(/^up ·/);
    expect(bars[1].title).toMatch(/^degraded ·/);
    expect(bars[2].title).toMatch(/^down ·/);
  });
});
