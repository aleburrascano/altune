import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import HeartbeatPanel, { type Data } from "./heartbeat.panel";
import { TokensContext, type TokenProvider } from "../api";
import * as api from "../api";
import type { Snapshot, State } from "../types";

vi.mock("uplot", () => ({
  default: function FakePlot() {
    return { setSize: () => {}, destroy: () => {} };
  },
}));

const tokens: TokenProvider = { get: async () => "owner-token", refresh: async () => "owner-token" };

afterEach(() => {
  vi.restoreAllMocks();
});

function snap(state: State, data: Data): Snapshot<Data> {
  return {
    id: "heartbeat",
    title: "Heartbeat",
    state,
    severity: "ok",
    headline: "",
    updatedAt: new Date().toISOString(),
    data,
  };
}

const ticks: Data = {
  ticks: [
    { at: "2026-01-01T00:00:00.000Z", kind: "tick", text: "alive at 2026-01-01T00:00:00Z" },
    { at: "2026-01-01T00:00:05.000Z", kind: "tick", text: "alive at 2026-01-01T00:00:05Z" },
  ],
};

describe("HeartbeatPanel", () => {
  it.each<State>(["live", "stale", "source_down"])("renders the %s state with ticks", (state) => {
    render(<HeartbeatPanel snapshot={snap(state, ticks)} range="1h" />);
    const label = state === "source_down" ? "SOURCE DOWN" : state.toUpperCase();
    expect(screen.getByText(label)).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    expect(screen.getByText("alive at 2026-01-01T00:00:05Z")).toBeInTheDocument();
  });

  it("renders cleanly with no ticks yet", () => {
    render(<HeartbeatPanel snapshot={snap("live", { ticks: [] })} range="1h" />);
    expect(screen.getByText("no ticks yet")).toBeInTheDocument();
    expect(screen.getByText("0")).toBeInTheDocument();
  });

  it("shows a source-down notice and dims the content while keeping the last-known ticks", () => {
    render(<HeartbeatPanel snapshot={snap("source_down", ticks)} range="1h" />);
    expect(screen.getByText(/go-api unreachable — showing last-known activity/)).toBeInTheDocument();
    expect(screen.getByText("alive at 2026-01-01T00:00:05Z")).toBeInTheDocument();
  });

  it("shows a stale notice while keeping the last-known ticks", () => {
    render(<HeartbeatPanel snapshot={snap("stale", ticks)} range="1h" />);
    expect(screen.getByText(/heartbeat is stale — showing last-known activity/)).toBeInTheDocument();
  });

  it("draws the tick-gap chart once the series read resolves", async () => {
    vi.spyOn(api, "fetchSeries").mockResolvedValue({
      bucket: "heartbeat",
      range: "1h",
      series: { tick_gap_ms: [{ at: "2026-01-01T00:00:00Z", v: 42 }] },
    });

    render(
      <TokensContext.Provider value={tokens}>
        <HeartbeatPanel snapshot={snap("live", ticks)} range="1h" />
      </TokensContext.Provider>,
    );

    await waitFor(() => expect(api.fetchSeries).toHaveBeenCalledWith(tokens, "heartbeat", "1h"));
    expect(screen.queryByText(/tick-gap history unavailable/)).toBeNull();
  });

  it("says the tick-gap history is unavailable when the series read fails", async () => {
    vi.spyOn(api, "fetchSeries").mockRejectedValue(new Error("boom"));

    render(
      <TokensContext.Provider value={tokens}>
        <HeartbeatPanel snapshot={snap("live", ticks)} range="1h" />
      </TokensContext.Provider>,
    );

    expect(await screen.findByText(/tick-gap history unavailable/)).toBeInTheDocument();
  });
});
