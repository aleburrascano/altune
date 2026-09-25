import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { Overview } from "./Overview";
import type { Snapshot } from "../types";

function snap(id: string, overrides: Partial<Snapshot> = {}): Snapshot {
  return {
    id,
    title: id,
    state: "live",
    severity: "ok",
    headline: "",
    updatedAt: "",
    data: {},
    ...overrides,
  };
}

describe("Overview tiles — sparklines", () => {
  it("renders a tile without spark cleanly, with no chart mounted", () => {
    const { container } = render(
      <MemoryRouter>
        <Overview snapshots={[snap("no-spark")]} conn="live" />
      </MemoryRouter>,
    );
    expect(screen.getByLabelText("Open no-spark")).toBeInTheDocument();
    expect(container.querySelector('[role="img"][aria-label="trend"]')).toBeNull();
  });

  it("renders a chart for a tile whose snapshot carries a spark", () => {
    const { container } = render(
      <MemoryRouter>
        <Overview
          snapshots={[snap("with-spark", { spark: [{ at: "2026-09-24T00:00:00Z", v: 1 }] })]}
          conn="live"
        />
      </MemoryRouter>,
    );
    expect(container.querySelector('[role="img"][aria-label="trend"]')).not.toBeNull();
  });

  it("dims a source_down tile instead of styling it like an error", () => {
    render(
      <MemoryRouter>
        <Overview snapshots={[snap("down", { state: "source_down", severity: "ok" })]} conn="live" />
      </MemoryRouter>,
    );
    expect(screen.getByLabelText("Open down").className).toContain("opacity-70");
  });
});
