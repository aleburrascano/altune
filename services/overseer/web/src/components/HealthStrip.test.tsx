import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import * as api from "../api";
import { TokensContext } from "../api";
import type { OverseerHealth, Snapshot } from "../types";
import { HealthStrip } from "./HealthStrip";

const tokens: api.TokenProvider = { get: async () => "t", refresh: async () => "t" };

function snap(id: string, severity: Snapshot["severity"], state: Snapshot["state"] = "live"): Snapshot {
  return { id, title: id, state, severity, headline: "", updatedAt: "", data: {} };
}

function health(overrides: Partial<OverseerHealth> = {}): OverseerHealth {
  return { lastCycle: "2026-09-24T00:00:00Z", bucketsOk: 3, bucketsFailed: 0, ...overrides };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("HealthStrip — the overview's health at a glance", () => {
  it("shows a credential failure as a login problem, not a go-api outage", async () => {
    vi.spyOn(api, "fetchHealth").mockResolvedValue(
      health({
        credential: {
          ok: false,
          consecutiveFailures: 5,
          persistFailed: false,
          passwordGrant: true,
          lastError: "refresh token expired",
        },
      }),
    );

    render(
      <TokensContext.Provider value={tokens}>
        <HealthStrip snapshots={[snap("reliability", "ok")]} />
      </TokensContext.Provider>,
    );

    await waitFor(() => expect(screen.getByText(/Login problem/)).toBeInTheDocument());
    expect(screen.getByText(/not a go-api outage/)).toBeInTheDocument();
    expect(screen.queryByText(/go-api is down/i)).not.toBeInTheDocument();
  });

  it("says nothing about the credential when it is healthy", async () => {
    vi.spyOn(api, "fetchHealth").mockResolvedValue(
      health({ credential: { ok: true, consecutiveFailures: 0, persistFailed: false, passwordGrant: true } }),
    );

    render(
      <TokensContext.Provider value={tokens}>
        <HealthStrip snapshots={[snap("reliability", "ok")]} />
      </TokensContext.Provider>,
    );

    await waitFor(() => expect(api.fetchHealth).toHaveBeenCalled());
    expect(screen.queryByText(/Login problem/)).not.toBeInTheDocument();
  });

  it("counts warn and critical buckets from the snapshots it is given", () => {
    render(
      <TokensContext.Provider value={tokens}>
        <HealthStrip snapshots={[snap("a", "critical"), snap("b", "warn"), snap("c", "ok")]} />
      </TokensContext.Provider>,
    );

    expect(screen.getByText("1 critical")).toBeInTheDocument();
    expect(screen.getByText("1 warn")).toBeInTheDocument();
    expect(screen.getByText("critical")).toBeInTheDocument();
  });

  it("renders without a token provider, without crashing", () => {
    render(<HealthStrip snapshots={[snap("a", "ok")]} />);
    expect(screen.getByText("all clear")).toBeInTheDocument();
  });
});
