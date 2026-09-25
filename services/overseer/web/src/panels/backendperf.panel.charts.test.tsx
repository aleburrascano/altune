import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import type uPlot from "uplot";
import BackendPerfPanel, { type Data } from "./backendperf.panel";
import { TokensContext, type TokenProvider } from "../api";
import type { Snapshot } from "../types";

const plots = vi.hoisted(() => [] as { opts: uPlot.Options; data: uPlot.AlignedData }[]);

vi.mock("uplot", () => ({
  default: function FakePlot(this: unknown, opts: uPlot.Options, data: uPlot.AlignedData) {
    plots.push({ opts, data });
    return { setSize: () => {}, destroy: () => {} };
  },
}));

beforeEach(() => {
  plots.length = 0;
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const tokens: TokenProvider = { get: async () => "owner-token", refresh: async () => "owner-token" };

const data: Data = {
  routes: [
    {
      route: "/v1/tracks",
      count: 200,
      error_rate: 0.01,
      error_samples: 200,
      p50: { ms: 12, overflow: false },
      p95: { ms: 90, overflow: false },
      p99: { ms: 150, overflow: false },
    },
  ],
  throughput: [{ at: new Date().toISOString(), kind: "throughput", text: "5.0 req/s (200 in window across 1 route(s))" }],
};

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

function stubSeries(res: Response) {
  const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(res);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function openPanel() {
  return render(
    <TokensContext.Provider value={tokens}>
      <BackendPerfPanel snapshot={snap(data)} range="1h" />
    </TokensContext.Provider>,
  );
}

describe("BackendPerfPanel charts", () => {
  it("charts the overall latency percentiles and throughput from the series endpoint", async () => {
    const fetchMock = stubSeries(
      new Response(
        JSON.stringify({
          bucket: "backendperf",
          range: "1h",
          series: {
            p50_ms: [{ at: "2026-09-01T12:00:00Z", v: 10 }],
            p95_ms: [{ at: "2026-09-01T12:00:00Z", v: 90 }],
            p99_ms: [{ at: "2026-09-01T12:00:00Z", v: 150 }],
            throughput_rps: [{ at: "2026-09-01T12:00:00Z", v: 5.5 }],
            "p95_ms:/v1/tracks": [{ at: "2026-09-01T12:00:00Z", v: 90 }],
          },
        }),
        { status: 200 },
      ),
    );

    openPanel();

    const latency = screen.getByRole("figure", { name: "p50, p95, p99" });
    const throughputChart = screen.getByRole("figure", { name: "Throughput" });
    await waitFor(() => expect(latency).toHaveTextContent("90 ms"));
    expect(throughputChart).toHaveTextContent("5.5 req/s");
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/api\/buckets\/backendperf\/series\?range=1h$/);
    await waitFor(() => expect(plots.length).toBeGreaterThan(0));
  });

  it("shows the empty notice on each chart when the series carries no history yet", async () => {
    stubSeries(new Response(JSON.stringify({ bucket: "backendperf", range: "1h", series: {} }), { status: 200 }));

    openPanel();

    await screen.findByRole("figure", { name: "p50, p95, p99" });
    expect(screen.getAllByText(/no history for this range yet/)).toHaveLength(2);
  });

  it("says history is unavailable when the series read fails, and keeps the panel", async () => {
    stubSeries(new Response("series read failed", { status: 503 }));

    openPanel();

    expect(await screen.findByText(/latency and throughput history unavailable/)).toBeInTheDocument();
    expect(screen.getByText("LIVE")).toBeInTheDocument();
    expect(screen.getByText("/v1/tracks")).toBeInTheDocument();
  });

  it("feeds the per-route p95 sparkline from the p95_ms:<route> series", async () => {
    stubSeries(
      new Response(
        JSON.stringify({
          bucket: "backendperf",
          range: "1h",
          series: { "p95_ms:/v1/tracks": [{ at: "2026-09-01T12:00:00Z", v: 90 }] },
        }),
        { status: 200 },
      ),
    );

    openPanel();

    await waitFor(() => expect(plots.length).toBeGreaterThan(0));
    const sparkline = plots.find((p) => p.data[1]?.length === 1 && p.data[1][0] === 90);
    expect(sparkline).toBeDefined();
  });
});
