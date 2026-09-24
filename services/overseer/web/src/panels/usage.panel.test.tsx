import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import UsagePanel, { type Data } from "./usage.panel";
import { panelFor } from "./registry";
import type { Snapshot, State } from "../types";

function snap(state: State, data: Data): Snapshot<Data> {
  return { id: "usage", title: "Usage", state, severity: "ok", headline: "", updatedAt: new Date().toISOString(), data };
}

// A search query is watched-app data; the payload carries a hostile string to
// prove it renders as escaped text, never as injected markup.
const data: Data = {
  searches: [
    { label: "<img src=x onerror=alert(1)>", count: 12 },
    { label: "miles davis", count: 5 },
  ],
  plays: [{ label: "play", count: 20 }],
  timeline: [
    { label: "10:00", count: 3 },
    { label: "10:01", count: 7 },
  ],
};

describe("UsagePanel", () => {
  it("is auto-discovered by the registry under the 'usage' id", () => {
    expect(panelFor("usage")).toBe(UsagePanel);
  });

  it.each<State>(["live", "stale", "source_down"])("renders the %s state", (state) => {
    const { container } = render(<UsagePanel snapshot={snap(state, data)} range="1h" />);
    const label = state === "source_down" ? "SOURCE DOWN" : state.toUpperCase();
    expect(screen.getByText(label)).toBeInTheDocument();
    // Rollups render in every state (never blank): headline totals + a search row.
    expect(screen.getByText("Top searches")).toBeInTheDocument();
    expect(container.textContent).toContain("miles davis");
    // Watched-app label is escaped: shown as text, no injected element.
    expect(container.querySelector("img")).toBeNull();
    expect(container.textContent).toContain("<img src=x onerror=alert(1)>");
  });

  it("shows the last-known usage on source_down (never blank), with a notice", () => {
    render(<UsagePanel snapshot={snap("source_down", data)} range="1h" />);
    expect(screen.getByText("SOURCE DOWN")).toBeInTheDocument();
    expect(screen.getByText(/go-api unreachable/)).toBeInTheDocument();
    expect(screen.getByText("Top searches")).toBeInTheDocument();
  });

  it("renders an empty payload cleanly rather than crashing", () => {
    render(<UsagePanel snapshot={snap("live", { searches: [], plays: [], timeline: [] })} range="1h" />);
    expect(screen.getByText("no usage yet")).toBeInTheDocument();
    expect(screen.getByText("LIVE")).toBeInTheDocument();
  });
});
