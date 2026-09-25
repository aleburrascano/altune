import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { CorrView } from "./CorrView";
import type { Snapshot } from "../types";

function snapshot(id: string, title: string, data: unknown): Snapshot {
  return {
    id,
    title,
    state: "live",
    severity: "ok",
    headline: "",
    updatedAt: new Date().toISOString(),
    data,
  };
}

function harness(buckets: Snapshot[], corrId: string) {
  return render(
    <MemoryRouter initialEntries={[`/corr/${encodeURIComponent(corrId)}`]}>
      <Routes>
        <Route path="/corr/:id" element={<CorrView buckets={buckets} />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("CorrView", () => {
  it("lists signals from every bucket that carries the id, ordered by time", () => {
    const reliability = snapshot("reliability", "Reliability", {
      history: [
        { at: "2024-01-01T00:05:00Z", kind: "probe", text: "up", corrId: "abc-123" },
        { at: "2024-01-01T00:00:00Z", kind: "probe", text: "down", corrId: "abc-123" },
      ],
    });
    const logs = snapshot("logs", "Logs", {
      records: [{ time: "2024-01-01T00:02:00Z", level: "ERROR", msg: "boom", attrs: { corr_id: "abc-123" } }],
    });
    const unrelated = snapshot("cost", "Cost", {
      history: [{ at: "2024-01-01T00:01:00Z", kind: "probe", text: "noise", corrId: "other-id" }],
    });

    harness([reliability, logs, unrelated], "abc-123");

    const rows = screen.getAllByRole("row").slice(1);
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveTextContent("down");
    expect(rows[1]).toHaveTextContent("boom");
    expect(rows[2]).toHaveTextContent("up");
    expect(screen.getAllByRole("link", { name: "Reliability" })[0]).toHaveAttribute("href", "/bucket/reliability");
    expect(screen.queryByText("noise")).not.toBeInTheDocument();
  });

  it("shows an empty state for an unknown correlation id", () => {
    const reliability = snapshot("reliability", "Reliability", {
      history: [{ at: "2024-01-01T00:00:00Z", kind: "probe", text: "up", corrId: "known-id" }],
    });

    harness([reliability], "does-not-exist");

    expect(screen.getByText(/no signals found for correlation id/)).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("renders an untrusted correlation id as plain text, not markup", () => {
    const reliability = snapshot("reliability", "Reliability", { history: [] });

    harness([reliability], "<script>alert(1)</script>");

    expect(document.querySelector("script")).toBeNull();
    expect(screen.getByText("corr <script>alert(1)</script>")).toBeInTheDocument();
  });
});
