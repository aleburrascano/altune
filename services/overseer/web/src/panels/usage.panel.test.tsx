import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import UsagePanel, { type Data } from "./usage.panel";
import { panelFor } from "./registry";
import type { Snapshot, State } from "../types";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import type uPlot from "uplot";
import { BucketDetail } from "../components/BucketDetail";
import { TokensContext, type TokenProvider } from "../api";
import userEvent from "@testing-library/user-event";

const plots = vi.hoisted(() => [] as { opts: uPlot.Options; data: uPlot.AlignedData }[]);

vi.mock("uplot", () => ({
  default: function FakePlot(this: unknown, opts: uPlot.Options, data: uPlot.AlignedData) {
    plots.push({ opts, data });
    return { setSize: () => {}, setData: () => {}, destroy: () => {} };
  },
}));

// These describes were separate files (each its own jsdom window) before they
// were gathered here, so reset the shared window/module state before every test
// to keep each describe independent of run order.
beforeEach(() => {
  window.history.replaceState(null, "", "/");
  plots.length = 0;
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

function snap(state: State, data: Data): Snapshot<Data> {
  return { id: "usage", title: "Usage", state, severity: "ok", headline: "", updatedAt: new Date().toISOString(), data };
}

// A search query is watched-app data; the payload carries a hostile string to
// prove it renders as escaped text, never as injected markup.
const data: Data = {
  searches: [
    { label: "<img src=x onerror=alert(1)>", count: 12 },
    { label: "miles davis", count: 5 },
  ],
  plays: [{ label: "play", count: 20 }],
  timeline: [
    { label: "10:00", count: 3 },
    { label: "10:01", count: 7 },
  ],
};

describe("UsagePanel", () => {
  it("is auto-discovered by the registry under the 'usage' id", () => {
    expect(panelFor("usage")).toBe(UsagePanel);
  });

  it.each<State>(["live", "stale", "source_down"])("renders the %s state", (state) => {
    const { container } = render(<UsagePanel snapshot={snap(state, data)} range="1h" />);
    const label = state === "source_down" ? "SOURCE DOWN" : state.toUpperCase();
    expect(screen.getByText(label)).toBeInTheDocument();
    // Rollups render in every state (never blank): headline totals + a search row.
    expect(screen.getByText("Top searches")).toBeInTheDocument();
    expect(container.textContent).toContain("miles davis");
    // Watched-app label is escaped: shown as text, no injected element.
    expect(container.querySelector("img")).toBeNull();
    expect(container.textContent).toContain("<img src=x onerror=alert(1)>");
  });

  it("shows the last-known usage on source_down (never blank), with a notice", () => {
    render(<UsagePanel snapshot={snap("source_down", data)} range="1h" />);
    expect(screen.getByText("SOURCE DOWN")).toBeInTheDocument();
    expect(screen.getByText(/go-api unreachable/)).toBeInTheDocument();
    expect(screen.getByText("Top searches")).toBeInTheDocument();
  });

  it("renders an empty payload cleanly rather than crashing", () => {
    render(<UsagePanel snapshot={snap("live", { searches: [], plays: [], timeline: [] })} range="1h" />);
    expect(screen.getByText("no usage yet")).toBeInTheDocument();
    expect(screen.getByText("LIVE")).toBeInTheDocument();
  });
});

describe("detail chart", () => {
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
});

describe("filtering", () => {
  function snap<T>(id: string, data: T): Snapshot<T> {
    return { id, title: id, state: "live", severity: "ok", headline: "", updatedAt: new Date().toISOString(), data };
  }

  beforeEach(() => {
    window.history.replaceState(null, "", "/");
  });

  describe("usage filtering", () => {
    const usage: Data = {
      searches: [
        { label: "miles davis", count: 5 },
        { label: "coltrane", count: 3 },
      ],
      plays: [{ label: "play", count: 20 }],
      timeline: [{ label: "10:00", count: 3 }],
    };

    it("filters rows by key and hides a toggled-off kind", async () => {
      render(<UsagePanel snapshot={snap("usage", usage)} range="1h" />);
      await userEvent.type(screen.getByLabelText("Filter by key"), "COLT");
      await waitFor(() => expect(screen.queryByText("miles davis")).toBeNull());
      expect(screen.getByText("coltrane")).toBeInTheDocument();
      await userEvent.click(screen.getByRole("button", { name: "searches" }));
      expect(screen.queryByText("Top searches")).toBeNull();
      expect(screen.getByText("Plays by kind")).toBeInTheDocument();
      expect(window.location.search).toBe("?q=COLT&kind=plays");
    });
  });
});
