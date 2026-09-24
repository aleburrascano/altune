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

describe("contract: Go wire JSON pinned against web/src/types.ts", () => {
  it("Snapshot covers every field the Go fixture (with reason and spark) carries", () => {
    const snapshot = snapshotFixture as unknown as Snapshot;
    // Destructuring against the Snapshot type, not the fixture, is the part that
    // fails to compile if a field is ever dropped from the type: TS errors here
    // the moment Snapshot stops declaring one of these names.
    const { id, title, state, severity, headline, reason, updatedAt, data, spark }: Snapshot = snapshot;

    expect(typeof id).toBe("string");
    expect(typeof title).toBe("string");
    expect(states).toContain(state);
    expect(severities).toContain(severity);
    expect(reason).toBeDefined();
    expect(reasons).toContain(reason);
    expect(typeof headline).toBe("string");
    expect(typeof updatedAt).toBe("string");
    expect(data).toBeDefined();
    expect(Array.isArray(spark)).toBe(true);
    expect(spark?.length).toBeGreaterThan(0);
  });

  it("SeriesResponse covers the minute-rollup min/max fields the Go fixture carries", () => {
    const series = seriesFixture as unknown as SeriesResponse;
    const { bucket, range, series: byName }: SeriesResponse = series;

    expect(typeof bucket).toBe("string");
    expect(ranges).toContain(range);
    const points: SeriesPoint[] = Object.values(byName)[0];
    expect(points.length).toBeGreaterThan(0);

    const { at, v, min, max }: SeriesPoint = points[0];
    expect(typeof at).toBe("string");
    expect(typeof v).toBe("number");
    expect(typeof min).toBe("number");
    expect(typeof max).toBe("number");
  });

  it("OverseerHealth covers the nested CredentialHealth the Go fixture carries", () => {
    const health: OverseerHealth = healthFixture;
    const { lastCycle, bucketsOk, bucketsFailed, credential }: OverseerHealth = health;

    expect(typeof lastCycle).toBe("string");
    expect(typeof bucketsOk).toBe("number");
    expect(typeof bucketsFailed).toBe("number");
    expect(credential).toBeDefined();

    const { ok, lastRefresh, consecutiveFailures, persistFailed, passwordGrant }: CredentialHealth = credential!;
    expect(typeof ok).toBe("boolean");
    expect(typeof lastRefresh).toBe("string");
    expect(typeof consecutiveFailures).toBe("number");
    expect(typeof persistFailed).toBe("boolean");
    expect(typeof passwordGrant).toBe("boolean");
  });
});
