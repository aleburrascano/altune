import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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

  it("omits the provider-calls chart when no provider has been observed yet", async () => {
    stubSeries(
      new Response(
        JSON.stringify({
          bucket: "cost",
          range: "1h",
          series: { spend_month_to_date: [{ at: "2026-09-01T12:00:00Z", v: 40 }], spend_daily: [] },
        }),
        { status: 200 },
      ),
    );

    render(
      <TokensContext.Provider value={tokens}>
        <CostPanel snapshot={{ ...snapshot(), data: { ...data, usage: {} } }} range="1h" />
      </TokensContext.Provider>,
    );

    await waitFor(() => expect(plots.length).toBeGreaterThan(0));
    expect(screen.queryByRole("figure", { name: "openai" })).toBeNull();
  });
});

describe("CostPanel spend table amount column", () => {
  function spendTable(): HTMLElement {
    return screen.getByRole("button", { name: "Amount" }).closest("table")!;
  }

  function serviceColumn(): string[] {
    return within(spendTable())
      .getAllByRole("row")
      .slice(1)
      .map((row) => within(row).getAllByRole("cell")[0].textContent ?? "");
  }

  function renderWithLines() {
    const spendData: Data = {
      ...data,
      spend: {
        ...data.spend!,
        lines: [
          { service: "COMPUTE", amount: 100 },
          { service: "STORAGE", amount: 5 },
          { service: "NETWORK", amount: 42.5 },
        ],
      },
    };
    return render(
      <TokensContext.Provider value={tokens}>
        <CostPanel snapshot={{ ...snapshot(), data: spendData }} range="1h" />
      </TokensContext.Provider>,
    );
  }

  it("shows the spend amount formatted as money", () => {
    renderWithLines();

    expect(screen.getByText("$100.00")).toBeInTheDocument();
    expect(screen.getByText("$5.00")).toBeInTheDocument();
    expect(screen.getByText("$42.50")).toBeInTheDocument();
  });

  it("sorts the amount column on the raw number, not the formatted text", async () => {
    const user = userEvent.setup();
    renderWithLines();

    await user.click(screen.getByRole("button", { name: "Amount" }));

    expect(serviceColumn()).toEqual(["STORAGE", "NETWORK", "COMPUTE"]);
  });
});
