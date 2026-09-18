import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import DomainQualityPanel, { type Data } from "./domainquality.panel";
import type { Snapshot, State } from "../types";

function snap(state: State, data: Data): Snapshot<Data> {
  return {
    id: "domainquality",
    title: "Domain quality",
    state,
    severity: "ok",
    headline: "",
    updatedAt: new Date().toISOString(),
    data,
  };
}

// A full payload with a hostile artist name to prove watched-app data is escaped.
const fullData: Data = {
  eval: {
    enabled: true,
    paused: false,
    state: "scored",
    score: 0.87,
    baseline: 0.8,
    last_run: new Date().toISOString(),
    queries: [],
  },
  evalStale: false,
  evalAgeStale: false,
  acquisition: {
    in_flight: 1,
    succeeded: 47,
    failed: 3,
    rejected: 0,
    queue_depth: 2,
    queue_capacity: 16,
  },
  acqStale: false,
  acqWindow: { rate: 0.9, completed: 10 },
  discography: {
    window_days: 30,
    group_by: "artist",
    suspect_rate: 0.42,
    last_sample_at: new Date().toISOString(),
    cases: [
      {
        artist: "<img src=x onerror=alert(1)>",
        artist_ref: "artist:evil",
        releases: 10,
        single_provider: 8,
        single_provider_no_id: 5,
        provider_counts: { spotify: 8, tidal: 2 },
        last_seen: new Date().toISOString(),
      },
      {
        artist: "Clean Artist",
        artist_ref: "artist:clean",
        releases: 20,
        single_provider: 1,
        single_provider_no_id: 0,
        provider_counts: { spotify: 19, tidal: 20 },
        last_seen: new Date().toISOString(),
      },
    ],
  },
  discoStale: false,
  discoTrend: [
    { at: new Date().toISOString(), kind: "discography", text: "top contamination 80% — <b>x</b> (8/10 suspects)" },
  ],
};

describe("DomainQualityPanel", () => {
  it.each<State>(["live", "stale", "source_down"])("renders the %s state cleanly", (state) => {
    const { container } = render(<DomainQualityPanel snapshot={snap(state, fullData)} />);
    const label = state === "source_down" ? "SOURCE DOWN" : state.toUpperCase();
    expect(screen.getByText(label)).toBeInTheDocument();
    // Bespoke content is present (not the generic JSON fallback).
    expect(container.querySelector(".panel-json")).toBeNull();
    expect(screen.getByText("0.87")).toBeInTheDocument();
    // The row percentage is the id-anchored no-id ratio (5/10), not the raw
    // single-provider headcount (8/10 = 80%).
    expect(container.textContent).toContain("50%");
    // The id-backing evidence rides each row: N/M single-provider, K without a shared id.
    expect(container.textContent).toContain("8/10 single-provider, 5 without a shared id");
  });

  it("renders the served suspect-rate headline with its last-sample age", () => {
    const { container } = render(<DomainQualityPanel snapshot={snap("live", fullData)} />);
    // The served rate (0.42) is rendered as a headline percentage beside its label,
    // and the last-sample age is shown so the headline's freshness reads honestly.
    expect(screen.getByText("42%")).toBeInTheDocument();
    expect(screen.getByText(/suspect rate/)).toBeInTheDocument();
    expect(container.textContent).toMatch(/sample \d+[smhd] ago/);
  });

  it("shows no-sample and em-dash markers when the served rate is absent", () => {
    const noRate: Data = {
      ...fullData,
      discography: { window_days: 30, group_by: "artist", cases: fullData.discography!.cases },
    };
    const { container } = render(<DomainQualityPanel snapshot={snap("live", noRate)} />);
    // An absent rate reads "—" (never a spurious 0%), an absent sample reads "no samples".
    expect(container.textContent).toContain("no samples");
  });

  it("renders the recent-window acquisition rate, not the lifetime ratio", () => {
    // Lifetime is a healthy 99% (990/1000), but the recent window is failing hard.
    // The panel must show the window's rate so the current spike is visible.
    const spiking: Data = {
      ...fullData,
      acquisition: { in_flight: 0, succeeded: 990, failed: 10, rejected: 0, queue_depth: 0, queue_capacity: 16 },
      acqWindow: { rate: 0.2, completed: 50 },
    };
    const { container } = render(<DomainQualityPanel snapshot={snap("live", spiking)} />);
    expect(screen.getByText("20%")).toBeInTheDocument();
    // The lifetime 99% never becomes the headline.
    expect(container.textContent).not.toContain("99%");
    // The window's completion count is shown as the rate's freshness.
    expect(container.textContent).toContain("50 recent");
  });

  it("shows no recent acquisition data when the window is empty", () => {
    const idle: Data = { ...fullData, acqWindow: null };
    const { container } = render(<DomainQualityPanel snapshot={snap("live", idle)} />);
    // An absent window reads "—" (never a spurious 0%) with a "no recent completions" note.
    expect(container.textContent).toContain("no recent completions");
  });

  it("flags the eval score stale by age with its last-run age, independent of read reachability", () => {
    const sixDaysAgo = new Date(Date.now() - 6 * 24 * 3600 * 1000).toISOString();
    const aged: Data = {
      ...fullData,
      evalStale: false, // the read is perfectly reachable
      evalAgeStale: true, // but the score itself is old
      eval: { ...fullData.eval!, last_run: sixDaysAgo },
    };
    const { container } = render(<DomainQualityPanel snapshot={snap("live", aged)} />);
    expect(container.textContent).toMatch(/ran \d+d ago/);
    expect(container.textContent).toContain("STALE");
  });

  it("renders the served worst-first order, never re-ranking by raw single-provider headcount", () => {
    // Headcount order and id-anchored order diverge: artist A is a single-provider
    // discography that is fully id-verified (headcount ratio 1.0, no-id ratio 0.0),
    // artist B is the real suspect (no-id ratio 0.3). go-api serves them worst-first
    // (B before A); the panel must render that order. A raw-headcount re-sort would
    // wrongly crown the id-verified A — the exact bug #1799 exists to kill.
    const divergent: Data = {
      ...fullData,
      discography: {
        window_days: 30,
        group_by: "artist",
        cases: [
          {
            artist: "No-Id B",
            artist_ref: "artist:b",
            releases: 10,
            single_provider: 3,
            single_provider_no_id: 3,
            provider_counts: { spotify: 7, tidal: 3 },
            last_seen: new Date().toISOString(),
          },
          {
            artist: "Id-Verified A",
            artist_ref: "artist:a",
            releases: 10,
            single_provider: 10,
            single_provider_no_id: 0,
            provider_counts: { spotify: 10 },
            last_seen: new Date().toISOString(),
          },
        ],
      },
    };
    const { container } = render(<DomainQualityPanel snapshot={snap("live", divergent)} />);
    const rows = container.querySelectorAll("ul li");
    expect(rows[0].textContent).toContain("No-Id B");
    expect(rows[0].textContent).not.toContain("Id-Verified A");
  });

  it("escapes watched-app artist names and trend text, never as markup", () => {
    const { container } = render(<DomainQualityPanel snapshot={snap("live", fullData)} />);
    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector("b")).toBeNull();
    expect(container.textContent).toContain("<img src=x onerror=alert(1)>");
    expect(container.textContent).toContain("<b>x</b>");
  });

  it("keeps showing last-known values on source_down (never blank)", () => {
    render(<DomainQualityPanel snapshot={snap("source_down", fullData)} />);
    expect(screen.getByText("SOURCE DOWN")).toBeInTheDocument();
    expect(screen.getByText(/go-api unreachable/)).toBeInTheDocument();
    expect(screen.getByText("Clean Artist")).toBeInTheDocument();
  });

  it("shows explicit no-data markers when the anchor reads are absent", () => {
    const empty: Data = {
      eval: null,
      evalStale: true,
      evalAgeStale: false,
      acquisition: null,
      acqStale: true,
      acqWindow: null,
      discography: null,
      discoStale: true,
      discoTrend: null,
    };
    const { container } = render(<DomainQualityPanel snapshot={snap("source_down", empty)} />);
    // Two em-dash placeholders (eval + acquisition), no crash on null payloads.
    expect(container.textContent).toContain("—");
    expect(screen.getByText("no rateable discographies")).toBeInTheDocument();
  });
});
