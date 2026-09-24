import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import type uPlot from "uplot";
import CostPanel, { type Data } from "./cost.panel";
import { TokensContext, type TokenProvider } from "../api";
import type { Snapshot } from "../types";

const plots = vi.hoisted(() => [] as { opts: uPlot.Options; data: uPlot.AlignedData }[]);

vi.mock("uplot", () => {
  const Fake = function FakePlot(this: unknown, opts: uPlot.Options, data: uPlot.AlignedData) {
    plots.push({ opts, data });
    return { setSize: () => {}, destroy: () => {} };
  } as unknown as { paths: { bars: () => unknown } } & typeof Function;
  Fake.paths = { bars: () => (): unknown => ({}) };
  return { default: Fake };
});

beforeEach(() => {
  plots.length = 0;
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const tokens: TokenProvider = { get: async () => "owner-token", refresh: async () => "owner-token" };

const data: Data = {
  spend: {
    amount: 42,
    currency: "USD",
    periodStart: new Date("2026-09-01T00:00:00Z").toISOString(),
    periodEnd: new Date("2026-09-15T00:00:00Z").toISOString(),
    lines: [{ service: "COMPUTE", amount: 42 }],
  },
  spendStale: false,
  spendUpdatedAt: new Date("2026-09-15T00:00:00Z").toISOString(),
  usage: { openai: { ok: 10, quota: 1, error: 0 } },
  usageStale: false,
};

function snapshot(): Snapshot<Data> {
  return {
    id: "cost",
    title: "Cost",
    state: "live",
    severity: "ok",
    headline: "",
    updatedAt: new Date().toISOString(),
    data,
  };
}

function renderCostPanel() {
  return render(
    <TokensContext.Provider value={tokens}>
      <CostPanel snapshot={snapshot()} range="1h" />
    </TokensContext.Provider>,
  );
}

function stubSeries(res: Response) {
  const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(res);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("CostPanel charts (fetchSeries)", () => {
  it("charts spend and provider-call history from the series endpoint", async () => {
    const fetchMock = stubSeries(
      new Response(
        JSON.stringify({
          bucket: "cost",
          range: "1h",
          series: {
            spend_month_to_date: [{ at: "2026-09-01T12:00:00Z", v: 40 }],
            spend_daily: [{ at: "2026-09-01T12:00:00Z", v: 2 }],
            "provider_calls:openai": [{ at: "2026-09-01T12:00:00Z", v: 11 }],
          },
        }),
        { status: 200 },
      ),
    );

    renderCostPanel();

    const trend = await screen.findByRole("figure", { name: "Month-to-date" });
    expect(trend).toBeInTheDocument();
    expect(screen.getByRole("figure", { name: "openai" })).toBeInTheDocument();
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/api\/buckets\/cost\/series\?range=1h$/);
    await waitFor(() => expect(plots.length).toBeGreaterThan(0));
  });

  it("shows a notice instead of crashing when the series read fails (OCI half down)", async () => {
    stubSeries(new Response("series read failed", { status: 503 }));

    renderCostPanel();

    expect(await screen.findAllByText(/history unavailable/)).not.toHaveLength(0);
    expect(screen.getByText("Infra spend (OCI)")).toBeInTheDocument();
    expect(screen.getByText("Provider API usage")).toBeInTheDocument();
  });

  it("omits the provider-calls chart when no provider has been observed yet", () => {
    render(<CostPanel snapshot={{ ...snapshot(), data: { ...data, usage: {} } }} />);
    expect(screen.queryByRole("figure")).toBeNull();
  });
});
