import { describe, expect, it } from "vitest";

import type {
  CredentialHealth,
  OverseerHealth,
  Range,
  Reason,
  Severity,
  SeriesPoint,
  SeriesResponse,
  Snapshot,
  State,
} from "../types";
import snapshotFixture from "./fixtures/snapshot.json";
import seriesFixture from "./fixtures/series.json";
import healthFixture from "./fixtures/health.json";

const states: State[] = ["live", "stale", "source_down"];
const severities: Severity[] = ["ok", "warn", "critical"];
const reasons: Reason[] = ["auth", "throttled", "degraded", "down", "connecting"];
const ranges: Range[] = ["1h", "24h", "7d"];

function assertKeys<T>(shape: Record<keyof T, true>, value: object, label: string): void {
  expect(Object.keys(value).sort(), label).toEqual(Object.keys(shape).sort());
}

const snapshotShape: Record<keyof Snapshot, true> = {
  id: true,
  title: true,
  state: true,
  reason: true,
  severity: true,
  headline: true,
  updatedAt: true,
  data: true,
  spark: true,
};

const seriesPointShape: Record<keyof SeriesPoint, true> = {
  at: true,
  v: true,
  min: true,
  max: true,
};

const seriesResponseShape: Record<keyof SeriesResponse, true> = {
  bucket: true,
  range: true,
  series: true,
};

const overseerHealthShape: Record<keyof OverseerHealth, true> = {
  lastCycle: true,
  bucketsOk: true,
  bucketsFailed: true,
  credential: true,
};

const credentialHealthShape: Record<keyof CredentialHealth, true> = {
  ok: true,
  lastRefresh: true,
  consecutiveFailures: true,
  persistFailed: true,
  passwordGrant: true,
  lastError: true,
};

describe("contract: Go wire JSON pinned against web/src/types.ts", () => {
  it("Snapshot covers every field the Go fixture (with reason and spark) carries", () => {
    assertKeys(snapshotShape, snapshotFixture, "Snapshot keys");
    const snapshot = snapshotFixture as Snapshot;
    const { id, title, state, severity, headline, reason, updatedAt, data, spark }: Snapshot = snapshot;

    expect(id).toBe("reliability");
    expect(title).toBe("Reliability");
    expect(states).toContain(state);
    expect(state).toBe("source_down");
    expect(severities).toContain(severity);
    expect(severity).toBe("warn");
    expect(reasons).toContain(reason);
    expect(reason).toBe("degraded");
    expect(headline).toBe("p95 latency 240ms");
    expect(updatedAt).toBe("2026-09-01T12:00:00Z");
    expect(data).toEqual({ p50: 80, p95: 240, p99: 410 });
    expect(spark).toEqual([{ at: "2026-09-01T12:00:00Z", v: 240 }]);
  });

  it("SeriesResponse covers the minute-rollup min/max fields the Go fixture carries", () => {
    assertKeys(seriesResponseShape, seriesFixture, "SeriesResponse keys");
    const series = seriesFixture as SeriesResponse;
    const { bucket, range, series: byName }: SeriesResponse = series;

    expect(bucket).toBe("reliability");
    expect(ranges).toContain(range);
    expect(range).toBe("7d");
    const points: SeriesPoint[] = Object.values(byName)[0];
    expect(points).toHaveLength(1);

    assertKeys(seriesPointShape, points[0], "SeriesPoint keys");
    const { at, v, min, max }: SeriesPoint = points[0];
    expect(at).toBe("2026-09-01T12:00:00Z");
    expect(v).toBe(240);
    expect(min).toBe(80);
    expect(max).toBe(410);
  });

  it("OverseerHealth covers the nested CredentialHealth the Go fixture carries", () => {
    assertKeys(overseerHealthShape, healthFixture, "OverseerHealth keys");
    const health: OverseerHealth = healthFixture;
    const { lastCycle, bucketsOk, bucketsFailed, credential }: OverseerHealth = health;

    expect(lastCycle).toBe("2026-09-01T12:00:00Z");
    expect(bucketsOk).toBe(8);
    expect(bucketsFailed).toBe(1);
    expect(credential).toBeDefined();

    assertKeys(credentialHealthShape, credential!, "CredentialHealth keys");
    const { ok, lastRefresh, consecutiveFailures, persistFailed, passwordGrant, lastError }: CredentialHealth =
      credential!;
    expect(ok).toBe(true);
    expect(lastRefresh).toBe("2026-09-01T12:00:00Z");
    expect(consecutiveFailures).toBe(0);
    expect(persistFailed).toBe(false);
    expect(passwordGrant).toBe(true);
    expect(lastError).toBe("invalid_grant");
  });
});
