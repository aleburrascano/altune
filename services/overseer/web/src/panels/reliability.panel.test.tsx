import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import ReliabilityPanel, { type Data } from "./reliability.panel";
import type { Snapshot, State } from "../types";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import type uPlot from "uplot";
import { BucketDetail } from "../components/BucketDetail";
import { TokensContext, type TokenProvider } from "../api";

const plots = vi.hoisted(() => [] as { opts: uPlot.Options; data: uPlot.AlignedData }[]);

vi.mock("uplot", () => ({
  default: function FakePlot(this: unknown, opts: uPlot.Options, data: uPlot.AlignedData) {
    plots.push({ opts, data });
    return { setSize: () => {}, destroy: () => {} };
  },
}));

function snap(state: State, data: Data): Snapshot<Data> {
  return {
    id: "reliability",
    title: "Reliability",
    state,
    severity: "ok",
    headline: "",
    updatedAt: new Date().toISOString(),
    data,
  };
}

const now = () => new Date().toISOString();

// Dependency error carries markup on purpose: it is watched-app data and must be
// escaped to text on render, never injected as an element.
const data: Data = {
  reachability: "up",
  health: {
    db: "up",
    redis: "up",
    auth: "down",
    detail: {
      db_latency_ms: 3,
      redis_latency_ms: 1,
      auth_latency_ms: 0,
      auth_error: "<img src=x onerror=alert(1)> auth timeout",
      checked_at: now(),
    },
    goroutines: 42,
    heap_mb: 17,
  },
  adminStale: false,
  history: [{ at: now(), kind: "health", text: "db=up redis=up auth=down (degraded)" }],
  poll: [
    { at: now(), kind: "reach", text: "up" },
    { at: now(), kind: "reach", text: "up" },
    { at: now(), kind: "reach", text: "down" },
    { at: now(), kind: "reach", text: "up" },
  ],
};

describe("ReliabilityPanel", () => {
  it.each<State>(["live", "stale", "source_down"])("renders the %s state cleanly", (state) => {
    const { container } = render(<ReliabilityPanel snapshot={snap(state, data)} range="1h" />);
    const label = state === "source_down" ? "SOURCE DOWN" : state.toUpperCase();
    expect(screen.getByText(label)).toBeInTheDocument();
    // The dependency pills render regardless of state (last-known on degrade).
    expect(container.textContent).toContain("Database");
    expect(container.textContent).toContain("Auth");
  });

  it("escapes watched-app dependency errors as text, never as markup", () => {
    const { container } = render(<ReliabilityPanel snapshot={snap("live", data)} range="1h" />);
    expect(container.querySelector("img")).toBeNull();
    expect(container.textContent).toContain("<img src=x onerror=alert(1)> auth timeout");
  });

  it("folds the own-poll ring into an uptime percentage", () => {
    // 3 of 4 probes up = 75%.
    const { container } = render(<ReliabilityPanel snapshot={snap("live", data)} range="1h" />);
    expect(container.textContent).toContain("75%");
  });

  it("keeps showing last-known health on source_down (never blank)", () => {
    render(<ReliabilityPanel snapshot={snap("source_down", data)} range="1h" />);
    expect(screen.getByText("SOURCE DOWN")).toBeInTheDocument();
    expect(screen.getByText(/go-api unreachable/)).toBeInTheDocument();
    // The mirrored history feed survives the source going down.
    expect(screen.getByText(/db=up redis=up auth=down/)).toBeInTheDocument();
  });

  it("renders before any health is mirrored without crashing", () => {
    const empty: Data = { reachability: "connecting", health: null, adminStale: false, history: [], poll: [] };
    const { container } = render(<ReliabilityPanel snapshot={snap("stale", empty)} range="1h" />);
    expect(screen.getByText("STALE")).toBeInTheDocument();
    expect(screen.getByText(/no dependency health mirrored yet/)).toBeInTheDocument();
    // No probes yet -> uptime is the explicit "no source" marker.
    expect(container.textContent).toContain("—");
  });
});

describe("detail charts", () => {
  beforeEach(() => {
    plots.length = 0;
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  const tokens: TokenProvider = { get: async () => "owner-token", refresh: async () => "owner-token" };

  const snapshot: Snapshot<Data> = {
    id: "reliability",
    title: "Reliability",
    state: "live",
    severity: "ok",
    headline: "uptime 100.0%",
    updatedAt: new Date().toISOString(),
    data: { reachability: "up", health: null, adminStale: false, history: [], poll: [] },
  };

  function openReliability() {
    return render(
      <TokensContext.Provider value={tokens}>
        <MemoryRouter initialEntries={["/bucket/reliability"]}>
          <Routes>
            <Route path="/bucket/:id" element={<BucketDetail snapshots={{ reliability: snapshot }} />} />
          </Routes>
        </MemoryRouter>
      </TokensContext.Provider>,
    );
  }

  function stubSeries(res: Response) {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(res);
    vi.stubGlobal("fetch", fetchMock);
    return fetchMock;
  }

  describe("Reliability detail charts", () => {
    it("shows uptime and latency charts for the last hour from the series endpoint", async () => {
      const fetchMock = stubSeries(
        new Response(
          JSON.stringify({
            bucket: "reliability",
            range: "1h",
            series: {
              up: [
                { at: "2026-09-01T12:00:00Z", v: 1 },
                { at: "2026-09-01T12:00:30Z", v: 0 },
              ],
              latency_ms: [{ at: "2026-09-01T12:00:00Z", v: 38.4 }],
            },
          }),
          { status: 200 },
        ),
      );

      openReliability();

      const uptime = await screen.findByRole("figure", { name: "Uptime" });
      const latency = screen.getByRole("figure", { name: "Latency" });
      expect(uptime).toHaveTextContent("down");
      expect(latency).toHaveTextContent("38 ms");
      expect(String(fetchMock.mock.calls[0][0])).toMatch(/api\/buckets\/reliability\/series\?range=1h$/);
      await waitFor(() => expect(plots).toHaveLength(2));
      expect(plots.map((p) => p.data[1])).toEqual([[1, 0], [38.4]]);
    });

    it("shows the empty notice on each chart when history holds no points yet", async () => {
      stubSeries(new Response(JSON.stringify({ bucket: "reliability", range: "1h", series: {} }), { status: 200 }));

      openReliability();

      await screen.findByRole("figure", { name: "Uptime" });
      expect(screen.getAllByText(/no history for this range yet/)).toHaveLength(2);
    });

    it("says history is unavailable when the series read fails, and keeps the panel", async () => {
      stubSeries(new Response("history read failed", { status: 503 }));

      openReliability();

      expect(await screen.findByText(/reachability history unavailable/)).toBeInTheDocument();
      expect(screen.getByText("Reachable")).toBeInTheDocument();
    });
  });
});

describe("on the panel kit", () => {
  function snap(state: State, data: Data): Snapshot<Data> {
    return {
      id: "reliability",
      title: "Reliability",
      state,
      severity: "ok",
      headline: "",
      updatedAt: new Date().toISOString(),
      data,
    };
  }

  const now = () => new Date().toISOString();

  const data: Data = {
    reachability: "up",
    health: {
      db: "up",
      redis: "up",
      auth: "down",
      detail: {
        db_latency_ms: 3,
        redis_latency_ms: 1,
        auth_latency_ms: 0,
        auth_error: "auth timeout",
        checked_at: now(),
      },
      goroutines: 42,
      heap_mb: 17,
    },
    adminStale: false,
    history: [{ at: now(), kind: "health", text: "db=up redis=up auth=down (degraded)" }],
    poll: [
      { at: now(), kind: "reach", text: "up" },
      { at: now(), kind: "reach", text: "up" },
      { at: now(), kind: "reach", text: "down" },
      { at: now(), kind: "reach", text: "up" },
    ],
  };

  describe("ReliabilityPanel rebuilt on the panel kit", () => {
    it("labels a degraded reachability with a warn tone instead of falling back to Unknown", () => {
      render(<ReliabilityPanel snapshot={snap("live", { ...data, reachability: "degraded" })} range="1h" />);

      const value = screen.getByText("Degraded");
      expect(value).toBeInTheDocument();
      expect(value.className).toContain("text-warn");
      expect(screen.queryByText("Unknown")).not.toBeInTheDocument();
    });

    it("renders the dependency mirror as a data table with headers and per-row status", () => {
      render(<ReliabilityPanel snapshot={snap("live", data)} range="1h" />);

      expect(screen.getByRole("columnheader", { name: "Dependency" })).toBeInTheDocument();
      expect(screen.getByRole("columnheader", { name: "Latency" })).toBeInTheDocument();
      const row = screen.getByRole("row", { name: /Auth/ });
      expect(row).toHaveTextContent("down");
      expect(row).toHaveTextContent("auth timeout");
    });

    it("renders no style attributes anywhere in the panel", () => {
      const { container } = render(<ReliabilityPanel snapshot={snap("live", data)} range="1h" />);
      expect(container.querySelectorAll("[style]")).toHaveLength(0);
    });

    it("shows the dependency table's own empty notice before any health is mirrored", () => {
      const empty: Data = { reachability: "connecting", health: null, adminStale: false, history: [], poll: [] };
      render(<ReliabilityPanel snapshot={snap("stale", empty)} range="1h" />);

      expect(screen.getByText("no dependency health mirrored yet")).toBeInTheDocument();
    });

    it("lists the mirrored history through the shared signal list", () => {
      render(<ReliabilityPanel snapshot={snap("live", data)} range="1h" />);

      expect(screen.getByText(/db=up redis=up auth=down \(degraded\)/)).toBeInTheDocument();
    });
  });
});
