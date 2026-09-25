import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import BackendPerfPanel, { type Data } from "./backendperf.panel";
import { panelFor } from "./registry";
import type { Snapshot, State } from "../types";

function snap(state: State, data: Data): Snapshot<Data> {
  return {
    id: "backendperf",
    title: "Back-end performance",
    state,
    severity: "ok",
    headline: "",
    updatedAt: new Date().toISOString(),
    data,
  };
}

// A route template carrying a would-be XSS payload as its name: it must render as
// escaped text (no injected element), proving the frontend-escaping invariant.
const data: Data = {
  routes: [
    {
      route: "/v1/tracks/{trackId}<img src=x onerror=alert(1)>",
      count: 1200,
      error_rate: 0.5,
      error_samples: 1200,
      p50: { ms: 12.3, overflow: false },
      p95: { ms: 240, overflow: false },
      p99: { ms: 800, overflow: true },
    },
    {
      route: "/health",
      count: 50,
      error_rate: 0,
      error_samples: 50,
      p50: { ms: 1.2, overflow: false },
      p95: { ms: 4, overflow: false },
      p99: { ms: 9, overflow: false },
    },
  ],
  throughput: [{ at: new Date().toISOString(), kind: "throughput", text: "12.5 req/s (150 in window across 2 route(s))" }],
};

describe("BackendPerfPanel", () => {
  it("is auto-discovered by the registry under the backendperf id", () => {
    expect(panelFor("backendperf")).toBe(BackendPerfPanel);
  });

  it.each<State>(["live", "stale", "source_down"])(
    "renders the %s state cleanly with the last-known latency",
    (state) => {
      const { container } = render(<BackendPerfPanel snapshot={snap(state, data)} range="1h" />);
      const label = state === "source_down" ? "SOURCE DOWN" : state.toUpperCase();
      expect(screen.getByText(label)).toBeInTheDocument();
      // Route templates are watched-app data: escaped, never injected as markup.
      expect(container.querySelector("img")).toBeNull();
      expect(container.textContent).toContain("onerror=alert(1)");
      // Latency is shown for every state (last-known preserved, never blank).
      expect(container.textContent).toContain("/health");
      // The overflow p99 estimate is marked as a lower bound.
      expect(container.textContent).toContain("≥");
    },
  );

  it("keeps showing the last-known latency on source_down with an explicit notice", () => {
    render(<BackendPerfPanel snapshot={snap("source_down", data)} range="1h" />);
    expect(screen.getByText("SOURCE DOWN")).toBeInTheDocument();
    expect(screen.getByText(/go-api unreachable/)).toBeInTheDocument();
  });

  it("surfaces the per-route 5xx error rate as a tile and a column", () => {
    const { container } = render(<BackendPerfPanel snapshot={snap("live", data)} range="1h" />);
    // The at-a-glance tile leads with the worst route's error rate.
    expect(screen.getByText("worst 5xx rate (window)")).toBeInTheDocument();
    expect(container.textContent).toContain("50.0%");
    // The failing route reads non-zero while the healthy /health route reads 0.0%.
    expect(container.textContent).toContain("0.0%");
  });

  it("marks a 5xx rate from a near-idle window as provisional", () => {
    const idle: Data = {
      routes: [
        { ...data.routes[1], route: "/v1/rare", count: 1, error_rate: 1, error_samples: 1 },
      ],
      throughput: [],
    };

    const { container } = render(<BackendPerfPanel snapshot={snap("live", idle)} range="1h" />);

    expect(container.textContent).toContain("100.0%?");
    expect(screen.getByText(/provisional/)).toBeInTheDocument();
  });

  it("leads the worst-rate tile with a graded route, not a near-idle 100%", () => {
    const mixed: Data = {
      routes: [
        { ...data.routes[1], route: "/v1/rare", count: 1, error_rate: 1, error_samples: 1 },
        { ...data.routes[1], route: "/v1/search", count: 200, error_rate: 0.2, error_samples: 200 },
      ],
      throughput: [],
    };

    render(<BackendPerfPanel snapshot={snap("live", mixed)} range="1h" />);

    const tile = screen.getByText("worst 5xx rate (window)").parentElement;
    expect(tile?.textContent).toContain("20.0%");
    expect(tile?.textContent).not.toContain("100.0%");
  });

  it("renders an empty state without crashing when there is no latency yet", () => {
    render(<BackendPerfPanel snapshot={snap("stale", { routes: [], throughput: [] })} range="1h" />);
    expect(screen.getByText("no route latency yet")).toBeInTheDocument();
  });

  it("labels latency and traffic as the recent window, not lifetime totals", () => {
    const { container } = render(<BackendPerfPanel snapshot={snap("live", data)} range="1h" />);
    expect(screen.getByText("latency and traffic reflect the recent window")).toBeInTheDocument();
    expect(screen.getByText("requests / window")).toBeInTheDocument();
    // The throughput trend reads as a per-second rate over the window.
    expect(container.textContent).toContain("req/s");
    expect(container.textContent).toContain("150 in window");
  });
});
