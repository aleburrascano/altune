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
      auth_error: "auth timeout",
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

describe("ReliabilityPanel rebuilt on the panel kit", () => {
  it("labels a degraded reachability with a warn tone instead of falling back to Unknown", () => {
    render(<ReliabilityPanel snapshot={snap("live", { ...data, reachability: "degraded" })} range="1h" />);

    const value = screen.getByText("Degraded");
    expect(value).toBeInTheDocument();
    expect(value.className).toContain("text-warn");
    expect(screen.queryByText("Unknown")).not.toBeInTheDocument();
  });

  it("renders the dependency mirror as a data table with headers and per-row status", () => {
    render(<ReliabilityPanel snapshot={snap("live", data)} range="1h" />);

    expect(screen.getByRole("columnheader", { name: "Dependency" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Latency" })).toBeInTheDocument();
    const row = screen.getByRole("row", { name: /Auth/ });
    expect(row).toHaveTextContent("down");
    expect(row).toHaveTextContent("auth timeout");
  });

  it("renders no style attributes anywhere in the panel", () => {
    const { container } = render(<ReliabilityPanel snapshot={snap("live", data)} range="1h" />);
    expect(container.querySelectorAll("[style]")).toHaveLength(0);
  });

  it("shows the dependency table's own empty notice before any health is mirrored", () => {
    const empty: Data = { reachability: "connecting", health: null, adminStale: false, history: [], poll: [] };
    render(<ReliabilityPanel snapshot={snap("stale", empty)} range="1h" />);

    expect(screen.getByText("no dependency health mirrored yet")).toBeInTheDocument();
  });

  it("lists the mirrored history through the shared signal list", () => {
    render(<ReliabilityPanel snapshot={snap("live", data)} range="1h" />);

    expect(screen.getByText(/db=up redis=up auth=down \(degraded\)/)).toBeInTheDocument();
  });
});
