import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import type uPlot from "uplot";
import LogsPanel, { type Data } from "./logs.panel";
import { TokensContext, type TokenProvider } from "../api";
import type { Snapshot, State } from "../types";

const plots = vi.hoisted(() => [] as { opts: uPlot.Options; data: uPlot.AlignedData }[]);

vi.mock("uplot", () => {
  const bars = vi.fn(() => () => ({}) as unknown);
  const Fake = function FakePlot(this: unknown, opts: uPlot.Options, data: uPlot.AlignedData) {
    plots.push({ opts, data });
    return { setSize: () => {}, setData: () => {}, destroy: () => {} };
  } as unknown as { paths: { bars: typeof bars } } & typeof Function;
  Fake.paths = { bars };
  return { default: Fake };
});

vi.mock("../api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api")>();
  return { ...actual, fetchSeries: vi.fn() };
});

import { fetchSeries } from "../api";

beforeEach(() => {
  plots.length = 0;
  vi.mocked(fetchSeries).mockReset();
});

function snap(state: State, data: Data): Snapshot<Data> {
  return { id: "logs", title: "Logs", state, severity: "ok", headline: "", updatedAt: new Date().toISOString(), data };
}

const data: Data = {
  minLevel: "DEBUG",
  dropped: 3,
  records: [
    { time: "2026-09-15T10:00:00Z", level: "INFO", msg: "server started", attrs: { port: "8080" } },
    { time: "2026-09-15T10:00:01Z", level: "WARN", msg: "slow query" },
    { time: "2026-09-15T10:00:02Z", level: "ERROR", msg: "boom" },
  ],
};

describe("LogsPanel on the kit", () => {
  it("renders the panel article with headline counts and the log tail table", () => {
    render(<LogsPanel snapshot={snap("live", data)} range="1h" />);
    expect(screen.getByRole("article", { name: "Logs" })).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument(); // lines
    expect(screen.getByText("server started")).toBeInTheDocument();
    expect(screen.getByText("slow query")).toBeInTheDocument();
    expect(screen.getByText("boom")).toBeInTheDocument();
    expect(screen.getByText("port=8080")).toBeInTheDocument();
  });

  it("surfaces a lossy notice when the bounded ring has dropped records", () => {
    render(<LogsPanel snapshot={snap("live", data)} range="1h" />);
    expect(screen.getByText(/3 log lines dropped/)).toBeInTheDocument();
  });

  it("stays silent about drops when nothing has been dropped", () => {
    render(<LogsPanel snapshot={snap("live", { ...data, dropped: 0 })} range="1h" />);
    expect(screen.queryByText(/dropped/)).toBeNull();
  });

  it("renders an empty tail cleanly", () => {
    render(<LogsPanel snapshot={snap("live", { records: [], minLevel: "DEBUG" })} range="1h" />);
    expect(screen.getByText("no logs yet")).toBeInTheDocument();
  });

  it.each<State>(["stale", "source_down"])("keeps showing the last-known tail on %s, behind a notice", (state) => {
    render(<LogsPanel snapshot={snap(state, data)} range="1h" />);
    expect(screen.getByText("server started")).toBeInTheDocument();
    const notice = state === "stale" ? /stream stalled/ : /go-api unreachable/;
    expect(screen.getByText(notice)).toBeInTheDocument();
  });

  it("renders a per-level chart once fetchSeries reports named series", async () => {
    vi.mocked(fetchSeries).mockResolvedValue({
      bucket: "logs",
      range: "1h",
      series: {
        error: [{ at: "2026-09-15T10:00:00Z", v: 1 }],
        warn: [{ at: "2026-09-15T10:00:00Z", v: 2 }],
      },
    });
    const tokens: TokenProvider = { get: async () => "owner-token", refresh: async () => "owner-token" };

    render(
      <TokensContext.Provider value={tokens}>
        <LogsPanel snapshot={snap("live", data)} range="1h" />
      </TokensContext.Provider>,
    );

    await waitFor(() => expect(plots).toHaveLength(1));
    expect(screen.getByRole("figure", { name: "Error, Warn" })).toBeInTheDocument();
  });

  it("shows no chart section when the bucket has no series yet", async () => {
    vi.mocked(fetchSeries).mockResolvedValue({ bucket: "logs", range: "1h", series: {} });
    const tokens: TokenProvider = { get: async () => "owner-token", refresh: async () => "owner-token" };

    render(
      <TokensContext.Provider value={tokens}>
        <LogsPanel snapshot={snap("live", data)} range="1h" />
      </TokensContext.Provider>,
    );

    await waitFor(() => expect(vi.mocked(fetchSeries)).toHaveBeenCalled());
    expect(screen.queryByText("Levels over time")).toBeNull();
    expect(plots).toHaveLength(0);
  });
});
