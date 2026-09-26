import { describe, it, expect } from "vitest";
import { render, screen, fireEvent, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, Navigate } from "react-router-dom";
import { Overview } from "./Overview";
import { BucketDetail } from "./BucketDetail";
import { compareWorstFirst } from "../lib/order";
import { overviewPath } from "../routes";
import type { Severity, Snapshot, State } from "../types";

function snap<D>(id: string, title: string, state: State, data: D): Snapshot<D> {
  return { id, title, state, severity: "ok", headline: "", updatedAt: new Date().toISOString(), data };
}

// A representative spread: a live bucket with a countable headline, a stale one, a
// down one, and a bucket with no obvious headline (state must still read cleanly).
const snapshots: Snapshot[] = [
  snap("liveactivity", "Live Activity", "live", {
    events: [{ at: "", kind: "track.played", text: "<img src=x onerror=alert(1)>" }],
    inFlight: 0,
    inFlightAvailable: false,
  }),
  snap("reliability", "Reliability", "stale", { incidents: [1, 2] }),
  snap("cost", "Cost", "source_down", { total: 12.5 }),
  snap("mystery", "Mystery", "live", {}),
];

const byId = Object.fromEntries(snapshots.map((s) => [s.id, s]));

// harness mirrors the Dashboard's routed content: overview at "/", drill-down at
// "/bucket/:id", so the test exercises real navigation, not a stub.
function harness(initial = "/") {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        <Route path={overviewPath} element={<Overview snapshots={snapshots} conn="live" />} />
        <Route path="/bucket/:id" element={<BucketDetail snapshots={byId} />} />
        <Route path="*" element={<Navigate to={overviewPath} replace />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("Overview — the landing grid", () => {
  it("renders every registered bucket with its live state", () => {
    harness();
    for (const s of snapshots) {
      expect(screen.getByText(s.title)).toBeInTheDocument();
    }
    // States are visible at the overview level, not only inside panels.
    expect(screen.getAllByText("LIVE").length).toBeGreaterThan(0);
    expect(screen.getByText("STALE")).toBeInTheDocument();
    expect(screen.getByText("SOURCE DOWN")).toBeInTheDocument();
  });

  it("shows a derived one-line summary, and state alone when there is no headline", () => {
    harness();
    expect(screen.getByText("1 events")).toBeInTheDocument();
    expect(screen.getByText("2 incidents")).toBeInTheDocument();
    // The mystery bucket has no headline: its summary is the "—" placeholder.
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("renders watched-app summary data as escaped text, never as markup", () => {
    const { container } = render(
      <MemoryRouter>
        <Overview
          snapshots={[snap("x", "X", "live", { note: "<script>alert(1)</script>" })]}
          conn="live"
        />
      </MemoryRouter>,
    );
    expect(container.querySelector("script")).toBeNull();
  });

  it("shows the API-unavailable message when there are no buckets and the API is down", () => {
    render(
      <MemoryRouter>
        <Overview snapshots={[]} conn="error" />
      </MemoryRouter>,
    );
    expect(screen.getByText("Overseer API unavailable.")).toBeInTheDocument();
  });
});

describe("drill-down navigation", () => {
  it("clicking a bucket drills into its full panel", () => {
    harness();
    fireEvent.click(screen.getByLabelText("Open Live Activity"));
    // The bespoke panel is now shown (its in-flight metric label), with a way back.
    expect(screen.getByText("in flight")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Overview/ })).toBeInTheDocument();
  });

  it("drills into the generic fallback for a bucket with no bespoke panel", () => {
    harness();
    fireEvent.click(screen.getByLabelText("Open Cost"));
    // Generic fallback renders the raw payload; the state badge is still shown.
    const back = screen.getByRole("link", { name: /Overview/ });
    expect(back).toBeInTheDocument();
    expect(screen.getByText("SOURCE DOWN")).toBeInTheDocument();
  });

  it("navigates back to the overview from a bucket detail", () => {
    harness();
    fireEvent.click(screen.getByLabelText("Open Reliability"));
    expect(screen.queryByLabelText("Open Cost")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("link", { name: /Overview/ }));
    // Back on the overview: every card is present again.
    expect(screen.getByLabelText("Open Cost")).toBeInTheDocument();
    expect(screen.getByLabelText("Open Reliability")).toBeInTheDocument();
  });

  it("shows a clean notice for an unknown bucket id rather than crashing", () => {
    harness("/bucket/does-not-exist");
    expect(screen.getByText(/No data for bucket/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Overview/ })).toBeInTheDocument();
  });
});

describe("overview card composition", () => {
  it("pairs each title with a state within the same card", () => {
    harness();
    const card = screen.getByLabelText("Open Cost");
    expect(within(card).getByText("Cost")).toBeInTheDocument();
    expect(within(card).getByText("SOURCE DOWN")).toBeInTheDocument();
  });
});

function graded(id: string, severity: Severity, state: State = "live"): Snapshot {
  return { id, title: id, state, severity, headline: "", updatedAt: "", data: {} };
}

describe("worst-first ordering (grid and nav share this comparator)", () => {
  it("floats the worst severity to the top, then freshness, then title", () => {
    const ordered = [
      graded("b-ok", "ok"),
      graded("a-critical", "critical"),
      graded("z-warn", "warn"),
      graded("a-ok-down", "ok", "source_down"),
    ]
      .sort(compareWorstFirst)
      .map((s) => s.id);
    expect(ordered).toEqual(["a-critical", "z-warn", "a-ok-down", "b-ok"]);
  });

  it("breaks a full severity+freshness tie by title", () => {
    const ordered = [graded("beta", "warn"), graded("alpha", "warn")]
      .sort(compareWorstFirst)
      .map((s) => s.id);
    expect(ordered).toEqual(["alpha", "beta"]);
  });
});

describe("severity drives the overview dot and badge color", () => {
  it("colors a reachable-but-critical bucket red while keeping its LIVE freshness label", () => {
    const { container } = render(
      <MemoryRouter>
        <Overview snapshots={[graded("crit", "critical", "live")]} conn="live" />
      </MemoryRouter>,
    );
    expect(container.querySelector(".dot-sev-critical")).not.toBeNull();
    expect(container.querySelector(".badge-sev-critical")).not.toBeNull();
    expect(screen.getByText("LIVE")).toBeInTheDocument();
  });
});

function tileSnap(id: string, overrides: Partial<Snapshot> = {}): Snapshot {
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
        <Overview snapshots={[tileSnap("no-spark")]} conn="live" />
      </MemoryRouter>,
    );
    expect(screen.getByLabelText("Open no-spark")).toBeInTheDocument();
    expect(container.querySelector('[role="img"][aria-label="trend"]')).toBeNull();
  });

  it("renders a chart for a tile whose snapshot carries a spark", () => {
    const { container } = render(
      <MemoryRouter>
        <Overview
          snapshots={[tileSnap("with-spark", { spark: [{ at: "2026-09-24T00:00:00Z", v: 1 }] })]}
          conn="live"
        />
      </MemoryRouter>,
    );
    expect(container.querySelector('[role="img"][aria-label="trend"]')).not.toBeNull();
  });

  it("dims a source_down tile instead of styling it like an error", () => {
    render(
      <MemoryRouter>
        <Overview snapshots={[tileSnap("down", { state: "source_down", severity: "ok" })]} conn="live" />
      </MemoryRouter>,
    );
    expect(screen.getByLabelText("Open down").className).toContain("opacity-70");
  });
});
