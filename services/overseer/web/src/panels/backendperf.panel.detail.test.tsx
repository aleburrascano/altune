import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import BackendPerfPanel, { type Data } from "./backendperf.panel";
import type { Snapshot } from "../types";

function snap(data: Data): Snapshot<Data> {
  return {
    id: "backendperf",
    title: "Back-end performance",
    state: "live",
    severity: "ok",
    headline: "",
    updatedAt: new Date().toISOString(),
    data,
  };
}

const provisional: Data = {
  routes: [
    {
      route: "/v1/rare",
      count: 1,
      error_rate: 1,
      error_samples: 1,
      p50: { ms: 1.2, overflow: false },
      p95: { ms: 4, overflow: false },
      p99: { ms: 9, overflow: false },
    },
  ],
  throughput: [],
};

describe("BackendPerfPanel detail", () => {
  it("shows the classified-response sample count for the worst 5xx rate without hovering a tooltip", () => {
    render(<BackendPerfPanel snapshot={snap(provisional)} range="1h" />);

    const tile = screen.getByText("worst 5xx rate (window)").parentElement;
    expect(tile?.textContent).toContain("classified response(s) this window");
    expect(tile?.textContent).toContain("1");
  });
});
