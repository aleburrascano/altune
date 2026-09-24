import { describe, it, expect } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
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
    const { container } = render(<DomainQualityPanel snapshot={snap(state, fullData)} range="1h" />);
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

  it("renders the served suspect-rate headline with its last-sample age", async () => {
    render(<DomainQualityPanel snapshot={snap("live", fullData)} range="1h" />);
    // The served rate (0.42) is rendered as a headline percentage beside its label,
    // and the last-sample age is shown in the metric's hint so the headline's freshness reads honestly.
    expect(screen.getByText("42%")).toBeInTheDocument();
    expect(screen.getByText(/suspect rate/)).toBeInTheDocument();
    const hint = screen.getByRole("button", { name: "About suspect rate" });
    hint.focus();
    await waitFor(() => expect(hint).toHaveAccessibleDescription(/sample \d+[smhd] ago/));
  });

  it("shows no-sample and em-dash markers when the served rate is absent", async () => {
    const noRate: Data = {
      ...fullData,
      discography: { window_days: 30, group_by: "artist", cases: fullData.discography!.cases },
    };
    render(<DomainQualityPanel snapshot={snap("live", noRate)} range="1h" />);
    // An absent rate reads "—" (never a spurious 0%), an absent sample reads "no samples" in the hint.
    const hint = screen.getByRole("button", { name: "About suspect rate" });
    hint.focus();
    await waitFor(() => expect(hint).toHaveAccessibleDescription(/no samples/));
  });

  it("renders the recent-window acquisition rate, not the lifetime ratio", async () => {
    // Lifetime is a healthy 99% (990/1000), but the recent window is failing hard.
    // The panel must show the window's rate so the current spike is visible.
    const spiking: Data = {
      ...fullData,
      acquisition: { in_flight: 0, succeeded: 990, failed: 10, rejected: 0, queue_depth: 0, queue_capacity: 16 },
      acqWindow: { rate: 0.2, completed: 50 },
    };
    const { container } = render(<DomainQualityPanel snapshot={snap("live", spiking)} range="1h" />);
    expect(screen.getByText("20%")).toBeInTheDocument();
    // The lifetime 99% never becomes the headline.
    expect(container.textContent).not.toContain("99%");
    // The window's completion count is shown as the rate's freshness, in the metric's hint.
    const hint = screen.getByRole("button", { name: "About acquisition" });
    hint.focus();
    await waitFor(() => expect(hint).toHaveAccessibleDescription(/50 recent/));
  });

  it("shows no recent acquisition data when the window is empty", async () => {
    const idle: Data = { ...fullData, acqWindow: null };
    render(<DomainQualityPanel snapshot={snap("live", idle)} range="1h" />);
    // An absent window reads "—" (never a spurious 0%) with a "no recent completions" note in the hint.
    const hint = screen.getByRole("button", { name: "About acquisition" });
    hint.focus();
    await waitFor(() => expect(hint).toHaveAccessibleDescription(/no recent completions/));
  });

  it("flags the eval score stale by age with its last-run age, independent of read reachability", async () => {
    const sixDaysAgo = new Date(Date.now() - 6 * 24 * 3600 * 1000).toISOString();
    const aged: Data = {
      ...fullData,
      evalStale: false, // the read is perfectly reachable
      evalAgeStale: true, // but the score itself is old
      eval: { ...fullData.eval!, last_run: sixDaysAgo },
    };
    render(<DomainQualityPanel snapshot={snap("live", aged)} range="1h" />);
    const hint = screen.getByRole("button", { name: "About eval score" });
    hint.focus();
    await waitFor(() => expect(hint).toHaveAccessibleDescription(/ran \d+d ago/));
    expect(hint).toHaveAccessibleDescription(/aged/);
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
    render(<DomainQualityPanel snapshot={snap("live", divergent)} range="1h" />);
    const rows = screen.getAllByRole("row").slice(1);
    expect(rows[0]).toHaveTextContent("No-Id B");
    expect(rows[0]).not.toHaveTextContent("Id-Verified A");
  });

  it("escapes watched-app artist names and trend text, never as markup", () => {
    const { container } = render(<DomainQualityPanel snapshot={snap("live", fullData)} range="1h" />);
    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector("b")).toBeNull();
    expect(container.textContent).toContain("<img src=x onerror=alert(1)>");
    expect(container.textContent).toContain("<b>x</b>");
  });

  it("keeps showing last-known values on source_down (never blank)", () => {
    render(<DomainQualityPanel snapshot={snap("source_down", fullData)} range="1h" />);
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
    const { container } = render(<DomainQualityPanel snapshot={snap("source_down", empty)} range="1h" />);
    // Two em-dash placeholders (eval + acquisition), no crash on null payloads.
    expect(container.textContent).toContain("—");
    expect(screen.getByText("no rateable discographies")).toBeInTheDocument();
  });
});
