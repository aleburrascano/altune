import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import CostPanel, { type Data } from "./cost.panel";
import { panelFor } from "./registry";
import type { Snapshot, State } from "../types";
import userEvent from "@testing-library/user-event";
import type uPlot from "uplot";
import { TokensContext, type TokenProvider } from "../api";

const plots = vi.hoisted(() => [] as { opts: uPlot.Options; data: uPlot.AlignedData }[]);

vi.mock("uplot", () => {
  const Fake = function FakePlot(this: unknown, opts: uPlot.Options, data: uPlot.AlignedData) {
    plots.push({ opts, data });
    return { setSize: () => {}, destroy: () => {} };
  } as unknown as { paths: { bars: () => unknown } } & typeof Function;
  Fake.paths = { bars: () => (): unknown => ({}) };
  return { default: Fake };
});

function snap(state: State, data: Data): Snapshot<Data> {
  return { id: "cost", title: "Cost", state, severity: "ok", headline: "", updatedAt: new Date().toISOString(), data };
}

// Service names (from OCI) and provider names (from go-api) are external source
// data; the payload carries a hostile string in each half to prove it renders as
// escaped text, never as injected markup.
const data: Data = {
  spend: {
    amount: 123.45,
    currency: "USD",
    periodStart: new Date("2026-09-01T00:00:00Z").toISOString(),
    periodEnd: new Date("2026-09-15T00:00:00Z").toISOString(),
    lines: [
      { service: "COMPUTE", amount: 100 },
      { service: "<img src=x onerror=alert(1)>", amount: 23.45 },
    ],
  },
  spendStale: false,
  usage: {
    openai: { ok: 40, quota: 3, error: 2 },
    "<script>alert(2)</script>": { ok: 5, quota: 0, error: 1 },
  },
  usageStale: false,
};

describe("CostPanel", () => {
  it("is auto-discovered by the registry under the 'cost' id", () => {
    expect(panelFor("cost")).toBe(CostPanel);
  });

  it.each<State>(["live", "stale", "source_down"])("renders the %s state", (state) => {
    const { container } = render(<CostPanel snapshot={snap(state, data)} range="1h" />);
    const label = state === "source_down" ? "SOURCE DOWN" : state.toUpperCase();
    // The overall state badge is present (getAllByText: per-half badges may repeat it).
    expect(screen.getAllByText(label).length).toBeGreaterThan(0);
    // Both halves render in every state (never blank): a spend line + a provider.
    expect(screen.getByText("Infra spend (OCI)")).toBeInTheDocument();
    expect(screen.getByText("Provider API usage")).toBeInTheDocument();
    expect(container.textContent).toContain("COMPUTE");
    expect(container.textContent).toContain("openai");
    // Watched-app labels are escaped: shown as text, no injected element.
    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector("script")).toBeNull();
    expect(container.textContent).toContain("<img src=x onerror=alert(1)>");
    expect(container.textContent).toContain("<script>alert(2)</script>");
  });

  it("shows both halves on source_down (never blank), with a notice", () => {
    render(<CostPanel snapshot={snap("source_down", { ...data, spendStale: true, usageStale: true })} range="1h" />);
    expect(screen.getByText(/both sources unreachable/)).toBeInTheDocument();
    expect(screen.getByText("Infra spend (OCI)")).toBeInTheDocument();
    expect(screen.getByText("Provider API usage")).toBeInTheDocument();
    expect(screen.getByText("COMPUTE")).toBeInTheDocument();
  });

  it("degrades the two halves independently", () => {
    // Spend stale (has last-known -> STALE), usage still live.
    render(
      <CostPanel snapshot={snap("stale", { ...data, spendStale: true, usageStale: false })} range="1h" />,
    );
    // Exactly one half shows STALE and one shows LIVE (plus the overall STALE badge).
    expect(screen.getAllByText("STALE").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("LIVE").length).toBeGreaterThanOrEqual(1);
  });

  it("renders an empty payload cleanly rather than crashing", () => {
    render(
      <CostPanel
        snapshot={snap("source_down", {
          spend: null,
          spendStale: true,
          usage: null,
          usageStale: true,
        })}
        range="1h"
      />,
    );
    expect(screen.getByText("no spend read yet")).toBeInTheDocument();
    expect(screen.getByText("no provider usage yet")).toBeInTheDocument();
    expect(screen.getAllByText("SOURCE DOWN").length).toBeGreaterThan(0);
  });
});

describe("charts and spend table", () => {
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
});
