import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import LogsPanel, { type Data } from "./logs.panel";
import { panelFor } from "./registry";
import type { Snapshot, State } from "../types";
import userEvent from "@testing-library/user-event";
import type uPlot from "uplot";
import { TokensContext, type TokenProvider, fetchSeries } from "../api";

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

function snap(state: State, data: Data): Snapshot<Data> {
  return { id: "logs", title: "Logs", state, severity: "ok", headline: "", updatedAt: new Date().toISOString(), data };
}

// Log message + attribute values are watched-app data; the payload carries a
// hostile string to prove it renders as escaped text, never as injected markup.
const data: Data = {
  minLevel: "DEBUG",
  records: [
    { time: "2026-09-15T10:00:00Z", level: "INFO", msg: "server started", attrs: { port: "8080" } },
    { time: "2026-09-15T10:00:01Z", level: "WARN", msg: "slow query" },
    {
      time: "2026-09-15T10:00:02Z",
      level: "ERROR",
      msg: "<img src=x onerror=alert(1)>",
      attrs: { evil: "<script>alert(2)</script>" },
    },
  ],
};

describe("LogsPanel", () => {
  it("is auto-discovered by the registry under the 'logs' id", () => {
    expect(panelFor("logs")).toBe(LogsPanel);
  });

  it.each<State>(["live", "stale", "source_down"])("renders the %s state", (state) => {
    const { container } = render(<LogsPanel snapshot={snap(state, data)} range="1h" />);
    const label = state === "source_down" ? "SOURCE DOWN" : state.toUpperCase();
    expect(screen.getByText(label)).toBeInTheDocument();
    // The tail renders in every state (never blank): a known line is present.
    expect(container.textContent).toContain("server started");
    expect(container.textContent).toContain("slow query");
    // Watched-app text is escaped: shown as text, no injected element.
    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector("script")).toBeNull();
    expect(container.textContent).toContain("<img src=x onerror=alert(1)>");
    expect(container.textContent).toContain("<script>alert(2)</script>");
  });

  it("shows the last-known logs on source_down (never blank), with a notice", () => {
    const { container } = render(<LogsPanel snapshot={snap("source_down", data)} range="1h" />);
    expect(screen.getByText("SOURCE DOWN")).toBeInTheDocument();
    expect(screen.getByText(/go-api unreachable/)).toBeInTheDocument();
    expect(container.textContent).toContain("server started");
  });

  it("renders an empty payload cleanly rather than crashing", () => {
    render(<LogsPanel snapshot={snap("live", { records: [], minLevel: "DEBUG" })} range="1h" />);
    expect(screen.getByText("no logs yet")).toBeInTheDocument();
    expect(screen.getByText("LIVE")).toBeInTheDocument();
  });
});

describe("on the panel kit", () => {
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
      expect(screen.getByText("3")).toBeInTheDocument();
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
});

describe("filtering", () => {
  function snap<T>(id: string, data: T): Snapshot<T> {
    return { id, title: id, state: "live", severity: "ok", headline: "", updatedAt: new Date().toISOString(), data };
  }

  const logs: Data = {
    minLevel: "DEBUG",
    records: [
      { time: "2026-09-15T10:00:00Z", level: "INFO", msg: "server started", attrs: { port: "8080" } },
      { time: "2026-09-15T10:00:01Z", level: "WARN", msg: "slow query" },
      { time: "2026-09-15T10:00:02Z", level: "ERROR", msg: "boom", attrs: { user: "Alice" } },
    ],
  };

  beforeEach(() => {
    window.history.replaceState(null, "", "/");
  });

  describe("logs filtering", () => {
    it("search narrows rows by message and attr value, case-insensitively", async () => {
      render(<LogsPanel snapshot={snap("logs", logs)} range="1h" />);
      await userEvent.type(screen.getByLabelText("Search logs"), "alice");
      await waitFor(() => expect(screen.queryByText("slow query")).toBeNull());
      expect(screen.getByText("boom")).toBeInTheDocument();
      expect(window.location.search).toBe("?q=alice");
    });

    it("level toggle hides that level", async () => {
      render(<LogsPanel snapshot={snap("logs", logs)} range="1h" />);
      await userEvent.click(screen.getByRole("button", { name: "WARN" }));
      expect(screen.queryByText("slow query")).toBeNull();
      expect(screen.getByText("boom")).toBeInTheDocument();
      expect(window.location.search).toBe("?level=ERROR%2CINFO%2CDEBUG");
    });

    it("restores search and levels from the URL on load", () => {
      window.history.replaceState(null, "", "/?q=slow&level=WARN");
      render(<LogsPanel snapshot={snap("logs", logs)} range="1h" />);
      expect(screen.getByText("slow query")).toBeInTheDocument();
      expect(screen.queryByText("boom")).toBeNull();
      expect(screen.getByLabelText("Search logs")).toHaveValue("slow");
    });

    it("renders only the visible rows of 5000 records", () => {
      const records = Array.from({ length: 5000 }, (_, i) => ({
        time: "2026-09-15T10:00:00Z",
        level: "INFO",
        msg: `line ${i}`,
      }));
      const { container } = render(<LogsPanel snapshot={snap("logs", { minLevel: "DEBUG", records })} range="1h" />);
      const rendered = container.querySelectorAll("li").length;
      expect(rendered).toBeGreaterThan(0);
      expect(rendered).toBeLessThan(60);
    });
  });
});
