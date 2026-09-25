import { afterEach, beforeEach, describe, it, expect, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { RelativeTime } from "./index";
import { formatAge } from "./RelativeTime";

const NOW = Date.parse("2026-09-23T12:00:00Z");

describe("RelativeTime", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders the age with the absolute time as its title", () => {
    const at = new Date(NOW - 4_000).toISOString();

    render(<RelativeTime at={at} />);

    const time = screen.getByText("4s ago");
    expect(time).toHaveAttribute("dateTime", at);
    expect(time).toHaveAttribute("title", new Date(NOW - 4_000).toLocaleString());
  });

  it("ticks forward every second", () => {
    render(<RelativeTime at={new Date(NOW - 4_000).toISOString()} />);

    act(() => {
      vi.advanceTimersByTime(2_000);
    });

    expect(screen.getByText("6s ago")).toBeInTheDocument();
  });

  it("stops its timer when unmounted", () => {
    const { unmount } = render(<RelativeTime at={new Date(NOW).toISOString()} />);

    unmount();

    expect(vi.getTimerCount()).toBe(0);
  });

  it.each(["", "not a date", "0001-01-01T00:00:00Z"])("reads %j as never", (at) => {
    render(<RelativeTime at={at} />);

    expect(screen.getByText("never")).toBeInTheDocument();
  });
});

describe("formatAge", () => {
  it.each<[number, string]>([
    [-5_000, "0s ago"],
    [0, "0s ago"],
    [59_999, "59s ago"],
    [60_000, "1m ago"],
    [3_599_999, "59m ago"],
    [3_600_000, "1h ago"],
    [86_399_999, "23h ago"],
    [86_400_000, "1d ago"],
    [3 * 86_400_000, "3d ago"],
  ])("renders an age of %d ms as %s", (ageMs, text) => {
    expect(formatAge(NOW - ageMs, NOW)).toBe(text);
  });
});
