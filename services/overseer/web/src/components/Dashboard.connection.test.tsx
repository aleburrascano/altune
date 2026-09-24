import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { SupabaseClient } from "@supabase/supabase-js";
import { Dashboard } from "./Dashboard";
import { fetchBuckets, openStream, type StreamHandlers } from "../api";
import { STALL_AFTER_MS } from "../hooks/useConnection";
import type { Reason, Snapshot } from "../types";

vi.mock("../api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api")>()),
  fetchBuckets: vi.fn(),
  openStream: vi.fn(),
}));

const STALLED_NOTICE = /Live updates stalled/;

function bucket(id: string, title: string, reason?: Reason): Snapshot {
  return {
    id,
    title,
    state: reason ? "stale" : "live",
    reason,
    severity: "ok",
    headline: "",
    updatedAt: new Date().toISOString(),
    data: {},
  };
}

const usage = bucket("usage", "Usage");

let stream: StreamHandlers;

beforeEach(() => {
  vi.useFakeTimers();
  vi.mocked(fetchBuckets).mockReset().mockResolvedValue([usage]);
  vi.mocked(openStream)
    .mockReset()
    .mockImplementation((_tokens, handlers) => {
      stream = handlers;
      return new Promise<void>(() => {});
    });
});

afterEach(() => {
  vi.useRealTimers();
});

async function renderDashboard() {
  render(
    <MemoryRouter>
      <Dashboard supabase={{} as SupabaseClient} onSignOut={vi.fn()} ownerEmail="owner@example.com" />
    </MemoryRouter>,
  );
  await act(() => vi.advanceTimersByTimeAsync(0));
}

async function elapse(ms: number) {
  await act(() => vi.advanceTimersByTimeAsync(ms));
}

function receive(snap: Snapshot) {
  act(() => stream.onSnapshot(snap));
}

function pills(conn: string) {
  return screen.queryAllByRole("status", { name: `Connection ${conn}` });
}

function tileRegion() {
  return screen.getByLabelText("Open Usage").closest("[aria-describedby], .opacity-100");
}

describe("connection pill", () => {
  it("returns to live when a frame arrives after a stream error", async () => {
    await renderDashboard();
    act(() => stream.onError?.(new Error("api/stream: HTTP 502")));
    expect(pills("error").length).toBeGreaterThan(0);

    receive(usage);

    expect(pills("error")).toHaveLength(0);
    expect(pills("live").length).toBeGreaterThan(0);
  });

  it("returns to live when the stream delivers after the first load failed", async () => {
    vi.mocked(fetchBuckets).mockRejectedValue(new Error("api/buckets: HTTP 502"));
    await renderDashboard();
    expect(pills("error").length).toBeGreaterThan(0);

    receive(usage);

    expect(pills("live").length).toBeGreaterThan(0);
    expect(screen.getByLabelText("Open Usage")).toBeInTheDocument();
  });

  it("names the state in text and in its accessible label, not by color alone", async () => {
    await renderDashboard();

    const [pill] = pills("live");

    expect(pill).toHaveTextContent("live");
    expect(pill).toHaveAccessibleName("Connection live");
  });
});

describe("stalled stream", () => {
  it("stays live through a silence just short of the stall window", async () => {
    await renderDashboard();

    await elapse(STALL_AFTER_MS - 1);

    expect(pills("live").length).toBeGreaterThan(0);
    expect(screen.queryByText(STALLED_NOTICE)).not.toBeInTheDocument();
  });

  it("shows stalled and dims the bucket tiles after the stall window of silence", async () => {
    await renderDashboard();

    await elapse(STALL_AFTER_MS);

    expect(pills("stalled").length).toBeGreaterThan(0);
    expect(pills("live")).toHaveLength(0);
    expect(tileRegion()).toHaveClass("opacity-50");
    expect(tileRegion()).toHaveAccessibleDescription(STALLED_NOTICE);
  });

  it("measures silence from the latest frame, not the first", async () => {
    await renderDashboard();
    await elapse(STALL_AFTER_MS - 1000);
    receive(usage);

    await elapse(STALL_AFTER_MS - 1);

    expect(pills("live").length).toBeGreaterThan(0);
  });

  it("restores live and undims the tiles when a new frame arrives", async () => {
    await renderDashboard();
    await elapse(STALL_AFTER_MS);

    receive(usage);

    expect(pills("live").length).toBeGreaterThan(0);
    expect(pills("stalled")).toHaveLength(0);
    expect(tileRegion()).not.toHaveClass("opacity-50");
    expect(screen.queryByText(STALLED_NOTICE)).not.toBeInTheDocument();
  });
});

describe("bucket reason", () => {
  it("shows why a bucket is stale on its overview tile", async () => {
    vi.mocked(fetchBuckets).mockResolvedValue([bucket("usage", "Usage", "throttled")]);
    await renderDashboard();

    expect(screen.getByLabelText("Open Usage")).toHaveTextContent("throttled");
  });
});
