import { describe, it, expect, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import LogsPanel, { type Data } from "./logs.panel";
import UsagePanel, { type Data as UsageData } from "./usage.panel";
import type { Snapshot } from "../types";

function snap<T>(id: string, data: T): Snapshot<T> {
  return { id, title: id, state: "live", severity: "ok", headline: "", updatedAt: new Date().toISOString(), data };
}

const logs: Data = {
  minLevel: "DEBUG",
  records: [
    { time: "2026-09-15T10:00:00Z", level: "INFO", msg: "server started", attrs: { port: "8080" } },
    { time: "2026-09-15T10:00:01Z", level: "WARN", msg: "slow query" },
    { time: "2026-09-15T10:00:02Z", level: "ERROR", msg: "boom", attrs: { user: "Alice" } },
  ],
};

beforeEach(() => {
  window.history.replaceState(null, "", "/");
});

describe("logs filtering", () => {
  it("search narrows rows by message and attr value, case-insensitively", async () => {
    render(<LogsPanel snapshot={snap("logs", logs)} range="1h" />);
    await userEvent.type(screen.getByLabelText("Search logs"), "alice");
    await waitFor(() => expect(screen.queryByText("slow query")).toBeNull());
    expect(screen.getByText("boom")).toBeInTheDocument();
    expect(window.location.search).toBe("?q=alice");
  });

  it("level toggle hides that level", async () => {
    render(<LogsPanel snapshot={snap("logs", logs)} range="1h" />);
    await userEvent.click(screen.getByRole("button", { name: "WARN" }));
    expect(screen.queryByText("slow query")).toBeNull();
    expect(screen.getByText("boom")).toBeInTheDocument();
    expect(window.location.search).toBe("?level=ERROR%2CINFO%2CDEBUG");
  });

  it("restores search and levels from the URL on load", () => {
    window.history.replaceState(null, "", "/?q=slow&level=WARN");
    render(<LogsPanel snapshot={snap("logs", logs)} range="1h" />);
    expect(screen.getByText("slow query")).toBeInTheDocument();
    expect(screen.queryByText("boom")).toBeNull();
    expect(screen.getByLabelText("Search logs")).toHaveValue("slow");
  });

  it("renders only the visible rows of 5000 records", () => {
    const records = Array.from({ length: 5000 }, (_, i) => ({
      time: "2026-09-15T10:00:00Z",
      level: "INFO",
      msg: `line ${i}`,
    }));
    const { container } = render(<LogsPanel snapshot={snap("logs", { minLevel: "DEBUG", records })} range="1h" />);
    const rendered = container.querySelectorAll("li").length;
    expect(rendered).toBeGreaterThan(0);
    expect(rendered).toBeLessThan(60);
  });
});

describe("usage filtering", () => {
  const usage: UsageData = {
    searches: [
      { label: "miles davis", count: 5 },
      { label: "coltrane", count: 3 },
    ],
    plays: [{ label: "play", count: 20 }],
    timeline: [{ label: "10:00", count: 3 }],
  };

  it("filters rows by key and hides a toggled-off kind", async () => {
    render(<UsagePanel snapshot={snap("usage", usage)} />);
    await userEvent.type(screen.getByLabelText("Filter by key"), "COLT");
    await waitFor(() => expect(screen.queryByText("miles davis")).toBeNull());
    expect(screen.getByText("coltrane")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "searches" }));
    expect(screen.queryByText("Top searches")).toBeNull();
    expect(screen.getByText("Plays by kind")).toBeInTheDocument();
    expect(window.location.search).toBe("?q=COLT&kind=plays");
  });
});
