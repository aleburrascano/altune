import { afterEach, describe, expect, it, vi } from "vitest";
import { fetchHealth, ForbiddenError, type TokenProvider } from "./api";

function tokenProvider(): TokenProvider {
  return { get: async () => "tok-1", refresh: async () => "tok-2" };
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("fetchHealth", () => {
  it("returns the overseer health envelope, credential included", async () => {
    const body = {
      lastCycle: "2026-09-24T00:00:00Z",
      bucketsOk: 7,
      bucketsFailed: 1,
      credential: { ok: false, consecutiveFailures: 3, persistFailed: false, passwordGrant: true },
    };
    vi.stubGlobal("fetch", vi.fn<typeof fetch>().mockResolvedValue(jsonResponse(200, body)));

    const got = await fetchHealth(tokenProvider());
    expect(got.bucketsFailed).toBe(1);
    expect(got.credential?.ok).toBe(false);
    expect(got.credential?.consecutiveFailures).toBe(3);
  });

  it("throws ForbiddenError for a non-owner token", async () => {
    vi.stubGlobal("fetch", vi.fn<typeof fetch>().mockResolvedValue(new Response("", { status: 403 })));
    await expect(fetchHealth(tokenProvider())).rejects.toBeInstanceOf(ForbiddenError);
  });
});
