import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import CostPanel, { type Data } from "./cost.panel";
import { panelFor } from "./registry";
import type { Snapshot, State } from "../types";

function snap(state: State, data: Data): Snapshot<Data> {
  return { id: "cost", title: "Cost", state, severity: "ok", headline: "", updatedAt: new Date().toISOString(), data };
}

// Service names (from OCI) and provider names (from go-api) are external source
// data; the payload carries a hostile string in each half to prove it renders as
// escaped text, never as injected markup.
const data: Data = {
  spend: {
    amount: 123.45,
    currency: "USD",
    periodStart: new Date("2026-09-01T00:00:00Z").toISOString(),
    periodEnd: new Date("2026-09-15T00:00:00Z").toISOString(),
    lines: [
      { service: "COMPUTE", amount: 100 },
      { service: "<img src=x onerror=alert(1)>", amount: 23.45 },
    ],
  },
  spendStale: false,
  usage: {
    openai: { ok: 40, quota: 3, error: 2 },
    "<script>alert(2)</script>": { ok: 5, quota: 0, error: 1 },
  },
  usageStale: false,
};

describe("CostPanel", () => {
  it("is auto-discovered by the registry under the 'cost' id", () => {
    expect(panelFor("cost")).toBe(CostPanel);
  });

  it.each<State>(["live", "stale", "source_down"])("renders the %s state", (state) => {
    const { container } = render(<CostPanel snapshot={snap(state, data)} range="1h" />);
    const label = state === "source_down" ? "SOURCE DOWN" : state.toUpperCase();
    // The overall state badge is present (getAllByText: per-half badges may repeat it).
    expect(screen.getAllByText(label).length).toBeGreaterThan(0);
    // Both halves render in every state (never blank): a spend line + a provider.
    expect(screen.getByText("Infra spend (OCI)")).toBeInTheDocument();
    expect(screen.getByText("Provider API usage")).toBeInTheDocument();
    expect(container.textContent).toContain("COMPUTE");
    expect(container.textContent).toContain("openai");
    // Watched-app labels are escaped: shown as text, no injected element.
    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector("script")).toBeNull();
    expect(container.textContent).toContain("<img src=x onerror=alert(1)>");
    expect(container.textContent).toContain("<script>alert(2)</script>");
  });

  it("shows both halves on source_down (never blank), with a notice", () => {
    render(<CostPanel snapshot={snap("source_down", { ...data, spendStale: true, usageStale: true })} range="1h" />);
    expect(screen.getByText(/both sources unreachable/)).toBeInTheDocument();
    expect(screen.getByText("Infra spend (OCI)")).toBeInTheDocument();
    expect(screen.getByText("Provider API usage")).toBeInTheDocument();
    expect(screen.getByText("COMPUTE")).toBeInTheDocument();
  });

  it("degrades the two halves independently", () => {
    // Spend stale (has last-known -> STALE), usage still live.
    render(
      <CostPanel snapshot={snap("stale", { ...data, spendStale: true, usageStale: false })} range="1h" />,
    );
    // Exactly one half shows STALE and one shows LIVE (plus the overall STALE badge).
    expect(screen.getAllByText("STALE").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("LIVE").length).toBeGreaterThanOrEqual(1);
  });

  it("renders an empty payload cleanly rather than crashing", () => {
    render(
      <CostPanel
        snapshot={snap("source_down", {
          spend: null,
          spendStale: true,
          usage: null,
          usageStale: true,
        })}
        range="1h"
      />,
    );
    expect(screen.getByText("no spend read yet")).toBeInTheDocument();
    expect(screen.getByText("no provider usage yet")).toBeInTheDocument();
    expect(screen.getAllByText("SOURCE DOWN").length).toBeGreaterThan(0);
  });
});
