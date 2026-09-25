import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import SecurityPanel, { type Data } from "./security.panel";
import { panelFor } from "./registry";
import type { Snapshot, State } from "../types";

function snap(state: State, data: Data): Snapshot<Data> {
  return { id: "security", title: "Security", state, severity: "ok", headline: "", updatedAt: new Date().toISOString(), data };
}

// A reflected probe error is watched-app data; the payload carries a hostile string
// to prove it renders as escaped text, never as injected markup.
const XSS = "<img src=x onerror=alert(1)>";

const data: Data = {
  hasRun: true,
  passed: 3,
  total: 4,
  lastRun: new Date().toISOString(),
  checks: [
    { name: "unauth-v1", desc: "unauthenticated /v1 read is rejected", reached: true, passed: true, status: 401 },
    { name: "observe-gate", desc: "unauthenticated /observe read is rejected", reached: true, passed: true, status: 403 },
    { name: "rate-limit-burst", desc: "a burst is shed or rejected", reached: true, passed: true, status: 429 },
    {
      name: "bad-input",
      desc: "a malformed read is rejected cleanly",
      reached: false,
      passed: false,
      status: 0,
      error: XSS,
    },
  ],
  history: [
    { at: new Date().toISOString(), kind: "selftest", text: "security self-test 3/4 passed" },
  ],
};

describe("SecurityPanel", () => {
  it("is auto-discovered by the registry under the 'security' id", () => {
    expect(panelFor("security")).toBe(SecurityPanel);
  });

  it.each<State>(["live", "stale", "source_down"])("renders the %s state", (state) => {
    const { container } = render(<SecurityPanel snapshot={snap(state, data)} range="1h" />);
    const label = state === "source_down" ? "SOURCE DOWN" : state.toUpperCase();
    expect(screen.getByText(label)).toBeInTheDocument();
    // Verdict + self-tests render in every state (never blank).
    expect(screen.getByText("Self-tests")).toBeInTheDocument();
    expect(container.textContent).toContain("unauth-v1");
    // Watched-app reflected error is escaped: shown as text, no injected element.
    expect(container.querySelector("img")).toBeNull();
    expect(container.textContent).toContain(XSS);
  });

  it("shows the last-known verdict on source_down (never blank), with a notice", () => {
    render(<SecurityPanel snapshot={snap("source_down", data)} range="1h" />);
    expect(screen.getByText("SOURCE DOWN")).toBeInTheDocument();
    expect(screen.getByText(/go-api unreachable/)).toBeInTheDocument();
    expect(screen.getByText("Self-tests")).toBeInTheDocument();
  });

  it("renders a no-run payload cleanly rather than crashing", () => {
    render(
      <SecurityPanel
        snapshot={snap("live", { hasRun: false, passed: 0, total: 0, lastRun: "", checks: [], history: [] })}
        range="1h"
      />,
    );
    expect(screen.getByText("no self-test run yet")).toBeInTheDocument();
    expect(screen.getByText("LIVE")).toBeInTheDocument();
  });
});
