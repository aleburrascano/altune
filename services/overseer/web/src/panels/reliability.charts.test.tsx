import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import type uPlot from "uplot";
import { BucketDetail } from "../components/BucketDetail";
import { TokensContext, type TokenProvider } from "../api";
import type { Snapshot } from "../types";
import type { Data } from "./reliability.panel";

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
