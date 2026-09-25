import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import type uPlot from "uplot";
import { BucketDetail } from "../components/BucketDetail";
import { TokensContext, type TokenProvider } from "../api";
import type { Snapshot } from "../types";
import type { Data } from "./security.panel";

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
  hasRun: true,
  passed: 2,
  total: 3,
  lastRun: "2026-09-23T12:00:00Z",
  checks: [
    { name: "unauth-v1", desc: "unauthenticated /v1 read is rejected", reached: true, passed: true, status: 401 },
    { name: "observe-gate", desc: "unauthenticated /observe read is rejected", reached: true, passed: true, status: 403 },
    { name: "rate-limit-burst", desc: "a burst is shed or rejected", reached: false, passed: false, status: 0, error: "dial tcp: timeout" },
  ],
  history: [
    { at: "2026-09-21T10:00:00Z", kind: "selftest", text: "security self-test 3/3 passed" },
    { at: "2026-09-22T09:00:00Z", kind: "selftest", text: "security self-test 2/3 passed" },
    { at: "2026-09-22T18:00:00Z", kind: "selftest", text: "security self-test 2/3 passed again" },
  ],
};

const snapshot: Snapshot<Data> = {
  id: "security",
  title: "Security",
  state: "live",
  severity: "warn",
  headline: "partial run",
  updatedAt: new Date().toISOString(),
  data,
};

function openSecurity(snap: Snapshot<Data> = snapshot) {
  return render(
    <TokensContext.Provider value={tokens}>
      <MemoryRouter initialEntries={["/bucket/security"]}>
        <Routes>
          <Route path="/bucket/:id" element={<BucketDetail snapshots={{ security: snap }} />} />
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

describe("SecurityPanel kit rebuild", () => {
  it("shows an open-findings/probe-failures trend fed by the series endpoint", async () => {
    const fetchMock = stubSeries(
      new Response(
        JSON.stringify({
          bucket: "security",
          range: "1h",
          series: {
            findings_open: [{ at: "2026-09-23T12:00:00Z", v: 1 }],
            probe_failures: [{ at: "2026-09-23T12:00:00Z", v: 2 }],
          },
        }),
        { status: 200 },
      ),
    );

    openSecurity();

    const trend = await screen.findByRole("figure");
    expect(trend).toHaveTextContent("Open findings");
    expect(trend).toHaveTextContent("Probe failures");
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/api\/buckets\/security\/series\?range=1h$/);
    await waitFor(() => expect(plots).toHaveLength(1));
    expect(plots[0].data[1]).toEqual([1]);
    expect(plots[0].data[2]).toEqual([2]);
  });

  it("says the trend is unavailable when the series read fails, and keeps the panel", async () => {
    stubSeries(new Response("series read failed", { status: 503 }));

    openSecurity();

    expect(await screen.findByText(/finding trend unavailable/)).toBeInTheDocument();
    expect(screen.getByText("Self-tests")).toBeInTheDocument();
  });

  it("lists open findings before passing checks in the self-tests table", async () => {
    stubSeries(new Response(JSON.stringify({ bucket: "security", range: "1h", series: {} }), { status: 200 }));

    openSecurity();
    await screen.findByText(/no history for this range yet/);

    const rows = screen.getAllByRole("row").slice(1);
    expect(within(rows[0]).getByText("rate-limit-burst")).toBeInTheDocument();
    expect(within(rows[0]).getByText("dial tcp: timeout")).toBeInTheDocument();
    expect(within(rows[0]).getByText("– unreached")).toBeInTheDocument();
    expect(within(rows[1]).getByText("unauth-v1")).toBeInTheDocument();
    expect(within(rows[2]).getByText("observe-gate")).toBeInTheDocument();
  });

  it("groups the run history into one section per day, newest day first", async () => {
    stubSeries(new Response(JSON.stringify({ bucket: "security", range: "1h", series: {} }), { status: 200 }));

    openSecurity();
    await screen.findByText(/no history for this range yet/);

    const headings = screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent);
    const septemberDays = headings.filter((h) => h?.includes("Sep"));
    expect(septemberDays).toEqual(["Sep 22, 2026", "Sep 21, 2026"]);
    const sep22 = screen.getByRole("region", { name: "Sep 22, 2026" });
    expect(within(sep22).getAllByRole("listitem")).toHaveLength(2);
    const sep21 = screen.getByRole("region", { name: "Sep 21, 2026" });
    expect(within(sep21).getAllByRole("listitem")).toHaveLength(1);
  });

  it("shows a clean history notice when no run has happened yet", () => {
    render(
      <TokensContext.Provider value={tokens}>
        <MemoryRouter initialEntries={["/bucket/security"]}>
          <Routes>
            <Route
              path="/bucket/:id"
              element={
                <BucketDetail
                  snapshots={{
                    security: {
                      ...snapshot,
                      data: { hasRun: false, passed: 0, total: 0, lastRun: "", checks: [], history: [] },
                    },
                  }}
                />
              }
            />
          </Routes>
        </MemoryRouter>
      </TokensContext.Provider>,
    );

    expect(screen.getByText("no self-test run yet")).toBeInTheDocument();
    expect(screen.queryByText("Self-tests")).toBeNull();
  });
});
