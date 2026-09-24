import { describe, it, expect, vi, afterEach } from "vitest";
import { fetchSeries, ForbiddenError, UnauthorizedError, type TokenProvider } from "./api";

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
