import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { panelFor, panels } from "./registry";
import registrySource from "./registry.tsx?raw";
import { GenericPanel } from "./GenericPanel";
import LiveActivityPanel from "./liveactivity.panel";
import type { Snapshot, State } from "../types";

describe("panel registry (additive on the frontend)", () => {
  it("auto-discovers the bespoke live-activity panel by id", () => {
    expect(panelFor("liveactivity")).toBe(LiveActivityPanel);
    expect(panels.liveactivity).toBeDefined();
  });

  it("falls back to the generic panel for an unknown bucket id", () => {
    expect(panelFor("brand-new-bucket")).toBe(GenericPanel);
  });

  it("core registry imports no concrete bucket panel by name (auto-discovery)", () => {
    // Panels are resolved by glob, not a hand-maintained import list.
    expect(registrySource).toContain("import.meta.glob");
    // No static import of a bespoke *.panel file, and no bucket panel named directly.
    expect(registrySource).not.toMatch(/^import\s+.*\.panel/m);
    expect(registrySource).not.toMatch(/LiveActivity/);
  });
});

function snap<D>(id: string, state: State, data: D): Snapshot<D> {
  return { id, title: id, state, severity: "ok", headline: "", updatedAt: new Date().toISOString(), data };
}

describe("GenericPanel renders all three states and escapes data", () => {
  it.each<State>(["live", "stale", "source_down"])("renders %s cleanly", (state) => {
    render(<GenericPanel snapshot={snap("x", state, { ok: true })} />);
    const label = state === "source_down" ? "SOURCE DOWN" : state.toUpperCase();
    expect(screen.getByText(label)).toBeInTheDocument();
  });

  it("renders watched-app data as escaped text, never as markup", () => {
    const { container } = render(
      <GenericPanel snapshot={snap("x", "live", { note: "<script>alert(1)</script>" })} />,
    );
    // React escapes on render: no live <script> element is injected.
    expect(container.querySelector("script")).toBeNull();
    expect(container.textContent).toContain("<script>alert(1)</script>");
  });
});

describe("LiveActivityPanel", () => {
  const data = {
    events: [
      { at: new Date().toISOString(), kind: "track.played", text: "<img src=x onerror=alert(1)>" },
    ],
    inFlight: 0,
    inFlightAvailable: false,
  };

  it.each<State>(["live", "stale", "source_down"])("renders the %s state", (state) => {
    const { container } = render(<LiveActivityPanel snapshot={snap("liveactivity", state, data)} range="1h" />);
    // Watched-app event text is escaped (no injected element), shown as text.
    expect(container.querySelector("img")).toBeNull();
    expect(container.textContent).toContain("<img src=x onerror=alert(1)>");
    // The in-flight signal shows the explicit "no source yet" marker.
    expect(container.textContent).toContain("—");
  });

  it("keeps showing the last-known feed on source_down (never blank)", () => {
    render(<LiveActivityPanel snapshot={snap("liveactivity", "source_down", data)} range="1h" />);
    expect(screen.getByText("SOURCE DOWN")).toBeInTheDocument();
    expect(screen.getByText(/go-api unreachable/)).toBeInTheDocument();
  });
});
