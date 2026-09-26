import { describe, it, expect, vi, afterEach } from "vitest";
import { fetchBuckets, openStream, type TokenProvider, fetchHealth, ForbiddenError, fetchSeries, UnauthorizedError } from "./api";
import type { Snapshot } from "./types";

afterEach(() => {
  vi.restoreAllMocks();
});

function tokenProvider(): TokenProvider & { refreshes: number } {
  const tp = {
    refreshes: 0,
    get: async () => "tok-1",
    refresh: async () => {
      tp.refreshes++;
      return "tok-2";
    },
  };
  return tp;
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("fetchBuckets — token expiry never blanks the API", () => {
  it("refreshes the token on a 401 and retries once", async () => {
    const tp = tokenProvider();
    const snaps: Snapshot[] = [{ id: "a", title: "A", state: "live", severity: "ok", headline: "", updatedAt: "", data: {} }];
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(new Response("", { status: 401 }))
      .mockResolvedValueOnce(jsonResponse(200, { buckets: snaps }));
    vi.stubGlobal("fetch", fetchMock);

    const got = await fetchBuckets(tp);
    expect(tp.refreshes).toBe(1);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(got).toHaveLength(1);
    expect(got[0].id).toBe("a");

    // The retry carried the refreshed token as a bearer header.
    const secondInit = fetchMock.mock.calls[1][1] as RequestInit;
    const headers = new Headers(secondInit.headers);
    expect(headers.get("Authorization")).toBe("Bearer tok-2");
    // Never sends cookies.
    expect(secondInit.credentials).toBe("omit");
  });
});

function sseStream(frames: string[]): ReadableStream<Uint8Array> {
  const enc = new TextEncoder();
  return new ReadableStream({
    start(controller) {
      for (const f of frames) controller.enqueue(enc.encode(f));
      controller.close();
    },
  });
}

describe("openStream — SSE fetch reader", () => {
  it("parses data frames into snapshots", async () => {
    const tp = tokenProvider();
    const snapA: Snapshot = { id: "a", title: "A", state: "live", severity: "ok", headline: "", updatedAt: "", data: {} };
    const snapB: Snapshot = { id: "b", title: "B", state: "source_down", severity: "ok", headline: "", updatedAt: "", data: {} };
    const body = sseStream([
      `data: ${JSON.stringify(snapA)}\n\n`,
      `data: ${JSON.stringify(snapB)}\n\n`,
    ]);
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(new Response(body, { status: 200, headers: { "Content-Type": "text/event-stream" } }));
    vi.stubGlobal("fetch", fetchMock);

    const got: Snapshot[] = [];
    const controller = new AbortController();
    const done = openStream(tp, { onSnapshot: (s) => got.push(s) }, controller.signal);
    // The single mocked connection closes; abort so the reconnect loop exits.
    await new Promise((r) => setTimeout(r, 20));
    controller.abort();
    await done;

    expect(got.map((s) => s.id)).toEqual(["a", "b"]);
    const init = fetchMock.mock.calls[0][1] as RequestInit;
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer tok-1");
    expect(init.credentials).toBe("omit");
  });

  it("refreshes and reconnects on a 401", async () => {
    const tp = tokenProvider();
    const snap: Snapshot = { id: "a", title: "A", state: "live", severity: "ok", headline: "", updatedAt: "", data: {} };
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(new Response("", { status: 401 }))
      .mockResolvedValueOnce(new Response(sseStream([`data: ${JSON.stringify(snap)}\n\n`]), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    const got: Snapshot[] = [];
    const controller = new AbortController();
    const done = openStream(tp, { onSnapshot: (s) => got.push(s) }, controller.signal);
    await new Promise((r) => setTimeout(r, 30));
    controller.abort();
    await done;

    expect(tp.refreshes).toBe(1);
    expect(got.map((s) => s.id)).toEqual(["a"]);
  });

  it("bounds reconnects when a refreshed token keeps getting 401", async () => {
    // Server rejects every token, including freshly refreshed ones. Without a
    // backoff on the refresh-retry path this is a tight loop hammering the server;
    // with it, the first retry is immediate and further ones back off.
    const tp = tokenProvider();
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(new Response("", { status: 401 }));
    vi.stubGlobal("fetch", fetchMock);

    const controller = new AbortController();
    const done = openStream(tp, { onSnapshot: () => {} }, controller.signal);
    await new Promise((r) => setTimeout(r, 60));
    controller.abort();
    await done;

    // A tight loop would issue hundreds of fetches in 60ms; the backoff keeps it to
    // the initial connect plus one immediate retry before it starts sleeping.
    expect(fetchMock.mock.calls.length).toBeLessThanOrEqual(3);
    expect(tp.refreshes).toBeGreaterThan(0);
  });
});

describe("health", () => {
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
});

describe("series", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function tokens(refreshed: string | null = "tok-2"): TokenProvider & { refreshes: number } {
    const tp = {
      refreshes: 0,
      get: async () => "tok-1",
      refresh: async () => {
        tp.refreshes++;
        return refreshed;
      },
    };
    return tp;
  }

  function stubFetch(...responses: Response[]) {
    const fetchMock = vi.fn<typeof fetch>();
    for (const res of responses) fetchMock.mockResolvedValueOnce(res);
    vi.stubGlobal("fetch", fetchMock);
    return fetchMock;
  }

  const body = {
    bucket: "reliability",
    range: "24h",
    series: { up: [{ at: "2026-09-01T12:00:00Z", v: 1 }] },
  };

  describe("fetchSeries", () => {
    it("asks for the bucket's series over the range with the bearer and no cookies", async () => {
      const fetchMock = stubFetch(new Response(JSON.stringify(body), { status: 200 }));

      const got = await fetchSeries(tokens(), "reliability", "24h");

      expect(got.series.up).toEqual([{ at: "2026-09-01T12:00:00Z", v: 1 }]);
      const [url, init] = fetchMock.mock.calls[0];
      expect(String(url)).toMatch(/api\/buckets\/reliability\/series\?range=24h$/);
      expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer tok-1");
      expect(init?.credentials).toBe("omit");
    });

    it("escapes the bucket id into one path segment", async () => {
      const fetchMock = stubFetch(new Response(JSON.stringify(body), { status: 200 }));

      await fetchSeries(tokens(), "../stream?x=1", "1h");

      expect(String(fetchMock.mock.calls[0][0])).toContain("api/buckets/..%2Fstream%3Fx%3D1/series?range=1h");
    });

    it("refreshes once on a 401 and retries with the fresh token", async () => {
      const tp = tokens();
      const fetchMock = stubFetch(new Response("", { status: 401 }), new Response(JSON.stringify(body), { status: 200 }));

      await fetchSeries(tp, "reliability", "1h");

      expect(tp.refreshes).toBe(1);
      expect(new Headers(fetchMock.mock.calls[1][1]?.headers).get("Authorization")).toBe("Bearer tok-2");
    });

    it("signals a lost session when the refresh fails", async () => {
      stubFetch(new Response("", { status: 401 }));
      await expect(fetchSeries(tokens(null), "reliability", "1h")).rejects.toBeInstanceOf(UnauthorizedError);
    });

    it("signals a non-owner on 403", async () => {
      stubFetch(new Response("", { status: 403 }));
      await expect(fetchSeries(tokens(), "reliability", "1h")).rejects.toBeInstanceOf(ForbiddenError);
    });

    it("rejects with the status on any other failure", async () => {
      stubFetch(new Response("history read failed", { status: 503 }));
      await expect(fetchSeries(tokens(), "reliability", "1h")).rejects.toThrow(/HTTP 503/);
    });

    it("treats a missing series map as no series", async () => {
      stubFetch(new Response(JSON.stringify({ bucket: "reliability", range: "1h" }), { status: 200 }));
      const got = await fetchSeries(tokens(), "reliability", "1h");
      expect(got.series).toEqual({});
    });
  });
});
