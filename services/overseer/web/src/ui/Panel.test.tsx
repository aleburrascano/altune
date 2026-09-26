import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { Panel, Section } from "./index";
import type { Snapshot } from "../types";

function snapshot(overrides: Partial<Snapshot> = {}): Snapshot {
  return {
    id: "reliability",
    title: "Reliability",
    state: "stale",
    reason: "auth",
    severity: "critical",
    headline: "99.2%",
    updatedAt: new Date().toISOString(),
    data: {},
    ...overrides,
  };
}

describe("Panel", () => {
  it("heads the body with its title, state badge and age, and names the bucket in the footer", () => {
    render(
      <Panel title="Reliability" snapshot={snapshot()} actions={<button type="button">Refresh</button>}>
        <p>body</p>
      </Panel>,
    );

    const panel = screen.getByRole("article", { name: "Reliability" });
    expect(panel).toHaveTextContent("STALE");
    expect(panel).toHaveTextContent("login problem");
    expect(panel).toHaveTextContent("0s ago");
    expect(screen.getByRole("button", { name: "Refresh" })).toBeInTheDocument();
    expect(screen.getByText("body")).toBeInTheDocument();
    expect(screen.getByRole("contentinfo")).toHaveTextContent("reliability");
  });
});

describe("Section", () => {
  it("labels its region with its title", () => {
    render(
      <Section title="Latency">
        <p>chart</p>
      </Section>,
    );

    expect(screen.getByRole("region", { name: "Latency" })).toHaveTextContent("chart");
  });
});
