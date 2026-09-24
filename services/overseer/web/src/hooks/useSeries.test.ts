import { act, renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import * as api from "../api";
import type { Range } from "../types";
import { useSeries } from "./useSeries";

const tokens: api.TokenProvider = { get: async () => "t", refresh: async () => "t" };

function wrapper({ children }: { children: ReactNode }) {
  return createElement(api.TokensContext.Provider, { value: tokens }, children);
}

function response(v: number) {
  return { bucket: "b", range: "1h" as Range, series: { s: [{ at: "2026-09-01T12:00:00Z", v }] } };
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe("useSeries", () => {
  it("starts idle then reports ready with the series", async () => {
    vi.spyOn(api, "fetchSeries").mockResolvedValue(response(1));
    const { result } = renderHook(() => useSeries("b", "1h"), { wrapper });
    expect(result.current.status).toBe("idle");
    await waitFor(() => expect(result.current.status).toBe("ready"));
    expect(result.current.series.s[0].v).toBe(1);
  });

  it("reports unavailable when the fetch fails with a 503", async () => {
    vi.spyOn(api, "fetchSeries").mockRejectedValue(new Error("api: HTTP 503"));
    const { result } = renderHook(() => useSeries("b", "1h"), { wrapper });
    await waitFor(() => expect(result.current.status).toBe("unavailable"));
    expect(result.current.series).toEqual({});
  });

  it("keeps ready data when a later poll fails", async () => {
    vi.useFakeTimers();
    const spy = vi.spyOn(api, "fetchSeries").mockResolvedValueOnce(response(1)).mockRejectedValue(new Error("x"));
    const { result } = renderHook(() => useSeries("b", "1h", 100), { wrapper });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(result.current.status).toBe("ready");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    expect(spy).toHaveBeenCalledTimes(2);
    expect(result.current.status).toBe("ready");
  });

  it("polls on the interval and stops after unmount", async () => {
    vi.useFakeTimers();
    const spy = vi.spyOn(api, "fetchSeries").mockResolvedValue(response(1));
    const { unmount } = renderHook(() => useSeries("b", "1h", 100), { wrapper });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(250);
    });
    expect(spy).toHaveBeenCalledTimes(3);
    unmount();
    await vi.advanceTimersByTimeAsync(500);
    expect(spy).toHaveBeenCalledTimes(3);
  });

  it("drops a stale response that lands after the range changed", async () => {
    let resolveStale: (v: ReturnType<typeof response>) => void = () => {};
    vi.spyOn(api, "fetchSeries")
      .mockReturnValueOnce(new Promise((r) => (resolveStale = r)))
      .mockResolvedValue(response(2));
    const { result, rerender } = renderHook(({ r }: { r: Range }) => useSeries("b", r), {
      wrapper,
      initialProps: { r: "1h" as Range },
    });
    rerender({ r: "24h" });
    await waitFor(() => expect(result.current.series.s?.[0].v).toBe(2));
    await act(async () => resolveStale(response(9)));
    expect(result.current.series.s[0].v).toBe(2);
  });

  it("refetches when the range changes", async () => {
    const spy = vi.spyOn(api, "fetchSeries").mockResolvedValue(response(1));
    const { rerender } = renderHook(({ r }: { r: Range }) => useSeries("b", r), {
      wrapper,
      initialProps: { r: "1h" as Range },
    });
    await waitFor(() => expect(spy).toHaveBeenCalledWith(tokens, "b", "1h"));
    rerender({ r: "24h" });
    await waitFor(() => expect(spy).toHaveBeenCalledWith(tokens, "b", "24h"));
  });
});
