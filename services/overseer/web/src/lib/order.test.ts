import { describe, it, expect } from "vitest";
import { compareWorstFirst, neighborBucketId, worstFirst } from "./order";
import type { Snapshot } from "../types";

function snapshot(overrides: Partial<Snapshot> = {}): Snapshot {
  return {
    id: "reliability",
    title: "Reliability",
    state: "live",
    severity: "ok",
    headline: "100%",
    updatedAt: new Date().toISOString(),
    data: {},
    ...overrides,
  };
}

describe("worstFirst", () => {
  it("ranks a critical bucket ahead of a warn bucket ahead of an ok bucket", () => {
    const ok = snapshot({ id: "ok-bucket", title: "Ok", severity: "ok" });
    const warn = snapshot({ id: "warn-bucket", title: "Warn", severity: "warn" });
    const critical = snapshot({ id: "critical-bucket", title: "Critical", severity: "critical" });

    expect(worstFirst([ok, warn, critical]).map((s) => s.id)).toEqual([
      "critical-bucket",
      "warn-bucket",
      "ok-bucket",
    ]);
  });

  it("breaks a severity tie by freshness, source_down worse than stale worse than live", () => {
    const live = snapshot({ id: "live-bucket", title: "Live", state: "live" });
    const stale = snapshot({ id: "stale-bucket", title: "Stale", state: "stale" });
    const down = snapshot({ id: "down-bucket", title: "Down", state: "source_down" });

    expect(worstFirst([live, stale, down]).map((s) => s.id)).toEqual([
      "down-bucket",
      "stale-bucket",
      "live-bucket",
    ]);
  });

  it("breaks a full tie by title", () => {
    const b = snapshot({ id: "b", title: "Bravo" });
    const a = snapshot({ id: "a", title: "Alpha" });

    expect(compareWorstFirst(a, b)).toBeLessThan(0);
  });
});

describe("neighborBucketId", () => {
  const ordered = [
    snapshot({ id: "critical-bucket", title: "Critical", severity: "critical" }),
    snapshot({ id: "warn-bucket", title: "Warn", severity: "warn" }),
    snapshot({ id: "ok-bucket", title: "Ok", severity: "ok" }),
  ];

  it("moves to the next bucket in worst-first order", () => {
    expect(neighborBucketId(ordered, "critical-bucket", 1)).toBe("warn-bucket");
  });

  it("moves to the previous bucket in worst-first order", () => {
    expect(neighborBucketId(ordered, "warn-bucket", -1)).toBe("critical-bucket");
  });

  it("wraps from the last bucket forward to the first", () => {
    expect(neighborBucketId(ordered, "ok-bucket", 1)).toBe("critical-bucket");
  });

  it("wraps from the first bucket back to the last", () => {
    expect(neighborBucketId(ordered, "critical-bucket", -1)).toBe("ok-bucket");
  });

  it("starts at the worst bucket when there is no current bucket", () => {
    expect(neighborBucketId(ordered, undefined, 1)).toBe("critical-bucket");
  });

  it("returns undefined when there are no buckets", () => {
    expect(neighborBucketId([], "anything", 1)).toBeUndefined();
  });
});
