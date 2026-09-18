import { describe, it, expect } from "vitest";
import { summarize } from "./summary";
import type { Snapshot, State } from "./types";

function snap<D>(data: D, state: State = "live"): Snapshot<D> {
  return { id: "x", title: "X", state, severity: "ok", headline: "", updatedAt: new Date().toISOString(), data };
}

describe("summarize — a resilient one-line headline from any snapshot", () => {
  it("counts the largest top-level collection, labelled by its key", () => {
    const data = { events: [1, 2, 3], inFlight: 0, routes: [1] };
    expect(summarize(snap(data))).toBe("3 events");
  });

  it("humanizes camelCase and snake_case keys", () => {
    expect(summarize(snap({ inFlightRequests: [1, 2] }))).toBe("2 in flight requests");
    expect(summarize(snap({ queue_depth: 5 }))).toBe("queue depth: 5");
  });

  it("falls back to the first meaningful scalar when there is no collection", () => {
    expect(summarize(snap({ score: 0.98, note: "x" }))).toBe("score: 0.98");
    expect(summarize(snap({ enabled: true }))).toBe("enabled: true");
  });

  it("shows state alone (empty summary) when there is no obvious headline", () => {
    expect(summarize(snap({}))).toBe("");
    expect(summarize(snap(null))).toBe("");
    expect(summarize(snap({ events: [] }))).toBe("");
  });

  it("handles scalar and array payloads without crashing", () => {
    expect(summarize(snap("just a string"))).toBe("just a string");
    expect(summarize(snap(42))).toBe("42");
    expect(summarize(snap([1, 2, 3, 4]))).toBe("4 items");
  });

  it("truncates a long string headline", () => {
    const long = "a".repeat(100);
    const out = summarize(snap({ message: long }));
    expect(out.startsWith("message: ")).toBe(true);
    expect(out.length).toBeLessThan("message: ".length + 60);
    expect(out.endsWith("…")).toBe(true);
  });
});

describe("summarize — the bucket's own headline wins over shape inference", () => {
  function withHeadline(headline: string): Snapshot {
    return { id: "x", title: "X", state: "live", severity: "ok", headline, updatedAt: "", data: { events: [1, 2, 3] } };
  }

  it("renders snapshot.headline verbatim when present, ignoring the payload shape", () => {
    // The payload would infer "3 events"; the bucket's own headline must win.
    expect(summarize(withHeadline("all clear"))).toBe("all clear");
  });

  it("truncates a long own headline", () => {
    const out = summarize(withHeadline("a".repeat(100)));
    expect(out.length).toBeLessThan(60);
    expect(out.endsWith("…")).toBe(true);
  });

  it("falls back to shape inference when the headline is empty or blank", () => {
    expect(summarize(withHeadline(""))).toBe("3 events");
    expect(summarize(withHeadline("   "))).toBe("3 events");
  });
});
