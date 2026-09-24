import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import type uPlot from "uplot";
import LiveActivityPanel, { type Data } from "./liveactivity.panel";
import { TokensContext, type TokenProvider } from "../api";
import type { Snapshot, State } from "../types";

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

function snap(state: State, data: Data): Snapshot<Data> {
  return {
    id: "liveactivity",
    title: "Live activity",
    state,
    severity: "ok",
    headline: "",
    updatedAt: new Date().toISOString(),
    data,
  };
}

const now = () => new Date().toISOString();

const data: Data = {
  events: [
    { at: now(), kind: "track.played", text: "<img src=x onerror=alert(1)> played" },
    { at: now(), kind: "queue.added", text: "queued a track", corrId: "abc123" },
  ],
  inFlight: 3,
  inFlightAvailable: true,
  dropped: 2,
};

const empty: Data = { events: [], inFlight: 0, inFlightAvailable: false };

function stubSeries(res: Response) {
  const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(res);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("LiveActivityPanel", () => {
  it.each<State>(["live", "stale", "source_down"])("renders the %s state cleanly", (state) => {
    const { container } = render(<LiveActivityPanel snapshot={snap(state, data)} range="1h" />);
    const label = state === "source_down" ? "SOURCE DOWN" : state.toUpperCase();
    expect(screen.getByText(label)).toBeInTheDocument();
    expect(container.querySelector("img")).toBeNull();
    expect(container.textContent).toContain("<img src=x onerror=alert(1)> played");
  });

  it("shows the empty-feed notice when there are no events yet", () => {
    render(<LiveActivityPanel snapshot={snap("live", empty)} range="1h" />);
    expect(screen.getByText("no events yet")).toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("keeps showing the last-known feed on source_down (never blank)", () => {
    render(<LiveActivityPanel snapshot={snap("source_down", data)} range="1h" />);
    expect(screen.getByText("SOURCE DOWN")).toBeInTheDocument();
    expect(screen.getByText(/go-api unreachable/)).toBeInTheDocument();
    expect(screen.getByText("queued a track")).toBeInTheDocument();
  });

  it("flags a lossy window and shows the corrId tag", () => {
    render(<LiveActivityPanel snapshot={snap("live", data)} range="1h" />);
    expect(screen.getByText(/2 event\(s\) dropped/)).toBeInTheDocument();
    expect(screen.getByText("corr abc123")).toBeInTheDocument();
  });

  it("fetches the event-volume series and feeds the chart", async () => {
    const fetchMock = stubSeries(
      new Response(
        JSON.stringify({
          bucket: "liveactivity",
          range: "1h",
          series: { events: [{ at: "2026-09-01T12:00:00Z", v: 4 }] },
        }),
        { status: 200 },
      ),
    );

    render(
      <TokensContext.Provider value={tokens}>
        <LiveActivityPanel snapshot={snap("live", data)} range="1h" />
      </TokensContext.Provider>,
    );

    const chart = await screen.findByRole("figure", { name: "Events" });
    expect(chart).toHaveTextContent("4");
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/api\/buckets\/liveactivity\/series\?range=1h$/);
    await waitFor(() => expect(plots).toHaveLength(1));
    expect(plots[0].data[1]).toEqual([4]);
  });

  it("says event history is unavailable when the series read fails, and keeps the panel", async () => {
    stubSeries(new Response("history read failed", { status: 503 }));

    render(
      <TokensContext.Provider value={tokens}>
        <LiveActivityPanel snapshot={snap("live", data)} range="1h" />
      </TokensContext.Provider>,
    );

    expect(await screen.findByText(/event history unavailable/)).toBeInTheDocument();
    expect(screen.getByText("LIVE")).toBeInTheDocument();
  });
});
