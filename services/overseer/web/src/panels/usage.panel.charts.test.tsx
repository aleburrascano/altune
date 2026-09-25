import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import type uPlot from "uplot";
import { BucketDetail } from "../components/BucketDetail";
import { TokensContext, type TokenProvider } from "../api";
import type { Snapshot } from "../types";
import type { Data } from "./usage.panel";

const plots = vi.hoisted(() => [] as { opts: uPlot.Options; data: uPlot.AlignedData }[]);

vi.mock("uplot", () => ({
  default: function FakePlot(this: unknown, opts: uPlot.Options, data: uPlot.AlignedData) {
    plots.push({ opts, data });
    return { setSize: () => {}, setData: () => {}, destroy: () => {} };
  },
}));

beforeEach(() => {
  plots.length = 0;
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const tokens: TokenProvider = { get: async () => "owner-token", refresh: async () => "owner-token" };

const emptyData: Data = { searches: [], plays: [], timeline: [] };

function snapshot(overrides: Partial<Snapshot<Data>> = {}): Snapshot<Data> {
  return {
    id: "usage",
    title: "Usage",
    state: "live",
    severity: "ok",
    headline: "12 searches, 20 plays",
    updatedAt: new Date().toISOString(),
    data: {
      searches: [{ label: "miles davis", count: 5 }],
      plays: [{ label: "play", count: 20 }],
      timeline: [{ label: "10:00", count: 3 }],
    },
    ...overrides,
  };
}

function openUsage(snap: Snapshot<Data>) {
  return render(
    <TokensContext.Provider value={tokens}>
      <MemoryRouter initialEntries={["/bucket/usage"]}>
        <Routes>
          <Route path="/bucket/:id" element={<BucketDetail snapshots={{ usage: snap }} />} />
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

describe("Usage detail chart", () => {
  it("charts requests/min and active users from the series endpoint", async () => {
    const fetchMock = stubSeries(
      new Response(
        JSON.stringify({
          bucket: "usage",
          range: "1h",
          series: {
            requests_per_min: [{ at: "2026-09-01T12:00:00Z", v: 14 }],
            active_users: [{ at: "2026-09-01T12:00:00Z", v: 3 }],
          },
        }),
        { status: 200 },
      ),
    );

    openUsage(snapshot());

    const figure = await screen.findByRole("figure", { name: "requests/min, active users" });
    expect(figure).toHaveTextContent("requests/min 14");
    expect(figure).toHaveTextContent("active users 3");
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/api\/buckets\/usage\/series\?range=1h$/);
    await waitFor(() => expect(plots).toHaveLength(1));
  });

  it("says history is unavailable when the series read fails, and keeps the panel", async () => {
    stubSeries(new Response("history read failed", { status: 503 }));

    openUsage(snapshot());

    expect(await screen.findByText(/usage history unavailable/)).toBeInTheDocument();
    expect(screen.getByText("Top searches")).toBeInTheDocument();
  });

  it("shows a lossy notice when the rollup dropped keys, alongside the last-known data", async () => {
    stubSeries(new Response(JSON.stringify({ bucket: "usage", range: "1h", series: {} }), { status: 200 }));

    openUsage(snapshot({ data: { ...snapshot().data, droppedKeys: 4 } }));

    expect(await screen.findByText(/4 low-count search\/play keys dropped/)).toBeInTheDocument();
  });

  it("renders cleanly with no rollups and no history yet", async () => {
    stubSeries(new Response(JSON.stringify({ bucket: "usage", range: "1h", series: {} }), { status: 200 }));

    openUsage(snapshot({ data: emptyData }));

    expect(screen.getByText("no usage yet")).toBeInTheDocument();
  });
});
