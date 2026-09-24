import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import HeartbeatPanel, { type Data } from "./heartbeat.panel";
import type { Snapshot, State } from "../types";

function snap(state: State, data: Data): Snapshot<Data> {
  return {
    id: "heartbeat",
    title: "Heartbeat",
    state,
    severity: "ok",
    headline: "",
    updatedAt: new Date().toISOString(),
    data,
  };
}

const ticks: Data = {
  ticks: [
    { at: "2026-01-01T00:00:00.000Z", kind: "tick", text: "alive at 2026-01-01T00:00:00Z" },
    { at: "2026-01-01T00:00:05.000Z", kind: "tick", text: "alive at 2026-01-01T00:00:05Z" },
  ],
};

describe("HeartbeatPanel", () => {
  it.each<State>(["live", "stale", "source_down"])("renders the %s state with ticks", (state) => {
    render(<HeartbeatPanel snapshot={snap(state, ticks)} range="1h" />);
    const label = state === "source_down" ? "SOURCE DOWN" : state.toUpperCase();
    expect(screen.getByText(label)).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    expect(screen.getByText("alive at 2026-01-01T00:00:05Z")).toBeInTheDocument();
  });

  it("renders cleanly with no ticks yet", () => {
    render(<HeartbeatPanel snapshot={snap("live", { ticks: [] })} range="1h" />);
    expect(screen.getByText("no ticks yet")).toBeInTheDocument();
    expect(screen.getByText("0")).toBeInTheDocument();
  });
});
