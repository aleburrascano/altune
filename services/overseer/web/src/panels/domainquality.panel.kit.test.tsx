import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import type uPlot from "uplot";
import { BucketDetail } from "../components/BucketDetail";
import { TokensContext, type TokenProvider } from "../api";
import type { Snapshot, State } from "../types";
import type { Data } from "./domainquality.panel";

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
    { at: "2026-09-21T10:00:00Z", kind: "discography", text: "top contamination 50% — Evil (5/10 suspects)" },
    { at: "2026-09-22T10:00:00Z", kind: "discography", text: "top contamination 40% — Evil (4/10 suspects)" },
  ],
};

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

function openDomainQuality(snapshot: Snapshot<Data>) {
  return render(
    <TokensContext.Provider value={tokens}>
      <MemoryRouter initialEntries={["/bucket/domainquality"]}>
        <Routes>
          <Route path="/bucket/:id" element={<BucketDetail snapshots={{ domainquality: snapshot }} />} />
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

describe("DomainQualityPanel kit rebuild", () => {
  it("renders the anchor metrics and the full discography trend for normal data", async () => {
    stubSeries(new Response(JSON.stringify({ bucket: "domainquality", range: "1h", series: {} }), { status: 200 }));

    openDomainQuality(snap("live", fullData));

    expect(screen.getByText("0.87")).toBeInTheDocument();
    expect(screen.getByText("90%")).toBeInTheDocument();
    expect(screen.getByText("42%")).toBeInTheDocument();
    await screen.findByText("top contamination 50% — Evil (5/10 suspects)");
    expect(screen.getByText("top contamination 40% — Evil (4/10 suspects)")).toBeInTheDocument();
  });

  it("escapes watched-app artist names, never as markup", async () => {
    stubSeries(new Response(JSON.stringify({ bucket: "domainquality", range: "1h", series: {} }), { status: 200 }));

    const { container } = openDomainQuality(snap("live", fullData));
    await screen.findByText("no history for this range yet");

    expect(container.querySelector("img")).toBeNull();
    expect(container.textContent).toContain("<img src=x onerror=alert(1)>");
  });

  it("charts the disco success, eval score, and acquisition series from the series endpoint", async () => {
    const fetchMock = stubSeries(
      new Response(
        JSON.stringify({
          bucket: "domainquality",
          range: "1h",
          series: {
            disco_success_rate: [{ at: "2026-09-23T12:00:00Z", v: 0.5 }],
            eval_score: [{ at: "2026-09-23T12:00:00Z", v: 0.87 }],
            acquisition_rate: [{ at: "2026-09-23T12:00:00Z", v: 0.9 }],
          },
        }),
        { status: 200 },
      ),
    );

    openDomainQuality(snap("live", fullData));

    const trend = await screen.findByRole("figure");
    expect(trend).toHaveTextContent("Disco success rate");
    expect(trend).toHaveTextContent("Eval score");
    expect(trend).toHaveTextContent("Acquisition rate");
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/api\/buckets\/domainquality\/series\?range=1h$/);
    await waitFor(() => expect(plots).toHaveLength(1));
    expect(plots[0].data[1]).toEqual([0.5]);
    expect(plots[0].data[2]).toEqual([0.87]);
    expect(plots[0].data[3]).toEqual([0.9]);
  });

  it("says the trend is unavailable when the series read fails, and keeps the panel", async () => {
    stubSeries(new Response("series read failed", { status: 503 }));

    openDomainQuality(snap("live", fullData));

    expect(await screen.findByText(/trend history unavailable/)).toBeInTheDocument();
    expect(screen.getByText("Discography · worst first")).toBeInTheDocument();
  });

  it("shows explicit no-data markers and no crash when every source is empty", async () => {
    stubSeries(new Response(JSON.stringify({ bucket: "domainquality", range: "1h", series: {} }), { status: 200 }));

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
    openDomainQuality(snap("source_down", empty));

    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
    expect(screen.getByText("no rateable discographies")).toBeInTheDocument();
    expect(screen.getByText("no discography trend yet")).toBeInTheDocument();
  });

  it("shows a stale notice per independent source without blanking the panel", async () => {
    stubSeries(new Response(JSON.stringify({ bucket: "domainquality", range: "1h", series: {} }), { status: 200 }));

    const partiallyStale: Data = { ...fullData, evalStale: true, acqStale: false, discoStale: false };
    openDomainQuality(snap("stale", partiallyStale));

    expect(screen.getByText("a source read is stale — showing last-known values.")).toBeInTheDocument();
    expect(screen.getByText("eval read is stale — showing last-known score.")).toBeInTheDocument();
    expect(screen.queryByText("acquisition read is stale — showing last-known status.")).toBeNull();
    expect(screen.queryByText("discography read is stale — showing last-known cases.")).toBeNull();
  });

  it("keeps showing last-known values on source_down, never blank", async () => {
    stubSeries(new Response(JSON.stringify({ bucket: "domainquality", range: "1h", series: {} }), { status: 200 }));

    openDomainQuality(snap("source_down", fullData));

    expect(screen.getByText("go-api unreachable — showing last-known domain quality.")).toBeInTheDocument();
    expect(await screen.findByText("Clean Artist")).toBeInTheDocument();
  });

  it("keeps the served worst-first discography order in the table", async () => {
    stubSeries(new Response(JSON.stringify({ bucket: "domainquality", range: "1h", series: {} }), { status: 200 }));

    openDomainQuality(snap("live", fullData));
    await screen.findByText("no history for this range yet");

    const rows = screen.getAllByRole("row").slice(1);
    expect(within(rows[0]).getByText("<img src=x onerror=alert(1)>")).toBeInTheDocument();
    expect(within(rows[1]).getByText("Clean Artist")).toBeInTheDocument();
  });

  it("shows the eval and acquisition facts as always-visible text, not only in the tooltip", async () => {
    stubSeries(new Response(JSON.stringify({ bucket: "domainquality", range: "1h", series: {} }), { status: 200 }));

    openDomainQuality(snap("live", fullData));

    expect(screen.getByText(/baseline 0\.80/)).toBeInTheDocument();
    expect(screen.getByText(/10 recent/)).toBeInTheDocument();
  });

  it("shows the eval score as visibly stale, with the word 'stale' and a warn tone, when aged", async () => {
    stubSeries(new Response(JSON.stringify({ bucket: "domainquality", range: "1h", series: {} }), { status: 200 }));

    const aged: Data = { ...fullData, evalAgeStale: true };
    openDomainQuality(snap("live", aged));

    const stale = screen.getByText(/· stale/);
    expect(stale.className).toContain("text-warn");
  });

  it("shows the eval meter's paused, disabled, and error facts", async () => {
    stubSeries(new Response(JSON.stringify({ bucket: "domainquality", range: "1h", series: {} }), { status: 200 }));

    const meterTrouble: Data = {
      ...fullData,
      eval: { ...fullData.eval!, state: "degraded", paused: true, enabled: false, error: "provider timeout" },
    };
    const { container } = openDomainQuality(snap("live", meterTrouble));
    await screen.findByText("no history for this range yet");

    expect(container.textContent).toContain("meter degraded");
    expect(container.textContent).toContain("paused");
    expect(container.textContent).toContain("disabled");
    expect(container.textContent).toContain("provider timeout");
  });

  it("shows a stale notice on the trend once a refetch fails after an earlier success", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ bucket: "domainquality", range: "1h", series: {} }), { status: 200 }),
      )
      .mockResolvedValue(new Response("series read failed", { status: 503 }));
    vi.stubGlobal("fetch", fetchMock);
    vi.useFakeTimers({ shouldAdvanceTime: true });

    openDomainQuality(snap("live", fullData));
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    await vi.advanceTimersByTimeAsync(30_000);
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    await vi.waitFor(() => expect(screen.getByText(/trend refresh failed/)).toBeInTheDocument());

    vi.useRealTimers();
  });
});
