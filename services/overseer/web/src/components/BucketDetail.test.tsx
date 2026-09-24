import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { BucketDetail } from "./BucketDetail";
import { TokensContext, type TokenProvider } from "../api";
import type { Snapshot } from "../types";
import type { Data } from "../panels/reliability.panel";

const tokens: TokenProvider = { get: async () => "owner-token", refresh: async () => "owner-token" };

const snapshot: Snapshot<Data> = {
  id: "reliability",
  title: "Reliability",
  state: "live",
  severity: "ok",
  headline: "uptime 100.0%",
  updatedAt: new Date().toISOString(),
  data: { reachability: "up", health: null, adminStale: false, history: [], poll: [] },
};

function stubSeries() {
  const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
    new Response(JSON.stringify({ bucket: "reliability", range: "1h", series: {} }), { status: 200 }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function harness(initial: string) {
  return render(
    <TokensContext.Provider value={tokens}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route path="/bucket/:id" element={<BucketDetail snapshots={{ reliability: snapshot }} />} />
        </Routes>
      </MemoryRouter>
    </TokensContext.Provider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("BucketDetail — full width with a range picker", () => {
  it("renders the panel at full width, with no leftover grid wrapper", () => {
    stubSeries();
    const { container } = harness("/bucket/reliability?range=7d");

    expect(container.querySelector(".grid-cell")).toBeNull();
    expect(container.querySelector(".detail > .grid")).toBeNull();
    expect(screen.getByText("Reliability")).toBeInTheDocument();
  });

  it("feeds the range from the URL into the panel", async () => {
    const fetchMock = stubSeries();
    harness("/bucket/reliability?range=7d");

    await waitFor(() =>
      expect(String(fetchMock.mock.calls[0][0])).toMatch(/api\/buckets\/reliability\/series\?range=7d$/),
    );
  });

  it("falls back to 1h for an unknown range in the URL", async () => {
    const fetchMock = stubSeries();
    harness("/bucket/reliability?range=nonsense");

    await waitFor(() =>
      expect(String(fetchMock.mock.calls[0][0])).toMatch(/api\/buckets\/reliability\/series\?range=1h$/),
    );
    expect(screen.getByRole("radio", { name: "1h" })).toHaveAttribute("aria-checked", "true");
  });

  it("clicking a range option changes the URL and re-feeds the panel", async () => {
    const fetchMock = stubSeries();
    const user = userEvent.setup();
    harness("/bucket/reliability?range=1h");

    await user.click(screen.getByRole("radio", { name: "24h" }));

    expect(screen.getByRole("radio", { name: "24h" })).toHaveAttribute("aria-checked", "true");
    await waitFor(() =>
      expect(String(fetchMock.mock.calls.at(-1)?.[0])).toMatch(/api\/buckets\/reliability\/series\?range=24h$/),
    );
  });

  it("is keyboard operable: arrow to a range and activate it with the keyboard", async () => {
    const fetchMock = stubSeries();
    const user = userEvent.setup();
    harness("/bucket/reliability?range=1h");

    await user.tab();
    await user.tab();
    expect(screen.getByRole("radio", { name: "1h" })).toHaveFocus();

    await user.keyboard("{ArrowRight}");
    expect(screen.getByRole("radio", { name: "24h" })).toHaveFocus();

    await user.keyboard("{Enter}");
    expect(screen.getByRole("radio", { name: "24h" })).toHaveAttribute("aria-checked", "true");
    await waitFor(() =>
      expect(String(fetchMock.mock.calls.at(-1)?.[0])).toMatch(/api\/buckets\/reliability\/series\?range=24h$/),
    );
  });

  it("shows a clean notice for an unknown bucket id, with no range picker", () => {
    harness("/bucket/does-not-exist");

    expect(screen.getByText(/No data for bucket/)).toBeInTheDocument();
    expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
  });
});
