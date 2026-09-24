import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import ReliabilityPanel, { type Data } from "./reliability.panel";
import type { Snapshot, State } from "../types";

function snap(state: State, data: Data): Snapshot<Data> {
  return {
    id: "reliability",
    title: "Reliability",
    state,
    severity: "ok",
    headline: "",
    updatedAt: new Date().toISOString(),
    data,
  };
}

const now = () => new Date().toISOString();

// Dependency error carries markup on purpose: it is watched-app data and must be
// escaped to text on render, never injected as an element.
const data: Data = {
  reachability: "up",
  health: {
    db: "up",
    redis: "up",
    auth: "down",
    detail: {
      db_latency_ms: 3,
      redis_latency_ms: 1,
      auth_latency_ms: 0,
      auth_error: "<img src=x onerror=alert(1)> auth timeout",
      checked_at: now(),
    },
    goroutines: 42,
    heap_mb: 17,
  },
  adminStale: false,
  history: [{ at: now(), kind: "health", text: "db=up redis=up auth=down (degraded)" }],
  poll: [
    { at: now(), kind: "reach", text: "up" },
    { at: now(), kind: "reach", text: "up" },
    { at: now(), kind: "reach", text: "down" },
    { at: now(), kind: "reach", text: "up" },
  ],
};

describe("ReliabilityPanel", () => {
  it.each<State>(["live", "stale", "source_down"])("renders the %s state cleanly", (state) => {
    const { container } = render(<ReliabilityPanel snapshot={snap(state, data)} range="1h" />);
    const label = state === "source_down" ? "SOURCE DOWN" : state.toUpperCase();
    expect(screen.getByText(label)).toBeInTheDocument();
    // The dependency pills render regardless of state (last-known on degrade).
    expect(container.textContent).toContain("Database");
    expect(container.textContent).toContain("Auth");
  });

  it("escapes watched-app dependency errors as text, never as markup", () => {
    const { container } = render(<ReliabilityPanel snapshot={snap("live", data)} range="1h" />);
    expect(container.querySelector("img")).toBeNull();
    expect(container.textContent).toContain("<img src=x onerror=alert(1)> auth timeout");
  });

  it("folds the own-poll ring into an uptime percentage", () => {
    // 3 of 4 probes up = 75%.
    const { container } = render(<ReliabilityPanel snapshot={snap("live", data)} range="1h" />);
    expect(container.textContent).toContain("75%");
  });

  it("keeps showing last-known health on source_down (never blank)", () => {
    render(<ReliabilityPanel snapshot={snap("source_down", data)} range="1h" />);
    expect(screen.getByText("SOURCE DOWN")).toBeInTheDocument();
    expect(screen.getByText(/go-api unreachable/)).toBeInTheDocument();
    // The mirrored history feed survives the source going down.
    expect(screen.getByText(/db=up redis=up auth=down/)).toBeInTheDocument();
  });

  it("renders before any health is mirrored without crashing", () => {
    const empty: Data = { reachability: "connecting", health: null, adminStale: false, history: [], poll: [] };
    const { container } = render(<ReliabilityPanel snapshot={snap("stale", empty)} range="1h" />);
    expect(screen.getByText("STALE")).toBeInTheDocument();
    expect(screen.getByText(/no dependency health mirrored yet/)).toBeInTheDocument();
    // No probes yet -> uptime is the explicit "no source" marker.
    expect(container.textContent).toContain("—");
  });
});
