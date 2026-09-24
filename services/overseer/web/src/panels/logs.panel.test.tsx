import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import LogsPanel, { type Data } from "./logs.panel";
import { panelFor } from "./registry";
import type { Snapshot, State } from "../types";

function snap(state: State, data: Data): Snapshot<Data> {
  return { id: "logs", title: "Logs", state, severity: "ok", headline: "", updatedAt: new Date().toISOString(), data };
}

// Log message + attribute values are watched-app data; the payload carries a
// hostile string to prove it renders as escaped text, never as injected markup.
const data: Data = {
  minLevel: "DEBUG",
  records: [
    { time: "2026-09-15T10:00:00Z", level: "INFO", msg: "server started", attrs: { port: "8080" } },
    { time: "2026-09-15T10:00:01Z", level: "WARN", msg: "slow query" },
    {
      time: "2026-09-15T10:00:02Z",
      level: "ERROR",
      msg: "<img src=x onerror=alert(1)>",
      attrs: { evil: "<script>alert(2)</script>" },
    },
  ],
};

describe("LogsPanel", () => {
  it("is auto-discovered by the registry under the 'logs' id", () => {
    expect(panelFor("logs")).toBe(LogsPanel);
  });

  it.each<State>(["live", "stale", "source_down"])("renders the %s state", (state) => {
    const { container } = render(<LogsPanel snapshot={snap(state, data)} range="1h" />);
    const label = state === "source_down" ? "SOURCE DOWN" : state.toUpperCase();
    expect(screen.getByText(label)).toBeInTheDocument();
    // The tail renders in every state (never blank): a known line is present.
    expect(container.textContent).toContain("server started");
    expect(container.textContent).toContain("slow query");
    // Watched-app text is escaped: shown as text, no injected element.
    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector("script")).toBeNull();
    expect(container.textContent).toContain("<img src=x onerror=alert(1)>");
    expect(container.textContent).toContain("<script>alert(2)</script>");
  });

  it("shows the last-known logs on source_down (never blank), with a notice", () => {
    const { container } = render(<LogsPanel snapshot={snap("source_down", data)} range="1h" />);
    expect(screen.getByText("SOURCE DOWN")).toBeInTheDocument();
    expect(screen.getByText(/go-api unreachable/)).toBeInTheDocument();
    expect(container.textContent).toContain("server started");
  });

  it("renders an empty payload cleanly rather than crashing", () => {
    render(<LogsPanel snapshot={snap("live", { records: [], minLevel: "DEBUG" })} range="1h" />);
    expect(screen.getByText("no logs yet")).toBeInTheDocument();
    expect(screen.getByText("LIVE")).toBeInTheDocument();
  });
});
