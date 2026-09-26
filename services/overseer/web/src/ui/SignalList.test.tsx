import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SignalList, type Signal } from "./index";

describe("SignalList", () => {
  const signals: Signal[] = [
    { at: "2026-09-23T12:00:00Z", kind: "health", text: "db degraded", corrId: "req-42" },
    { at: "2026-09-23T12:00:05Z", kind: "poll", text: "<script>alert(1)</script>" },
  ];

  it("lists each signal's kind and text, escaping hostile text", () => {
    const { container } = render(<SignalList signals={signals} empty="no signals" />);

    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    expect(screen.getByText("db degraded")).toBeInTheDocument();
    expect(screen.getByText("<script>alert(1)</script>")).toBeInTheDocument();
    expect(container.querySelector("script")).toBeNull();
  });

  it("hands a clicked correlation id to onCorrId", async () => {
    const user = userEvent.setup();
    const onCorrId = vi.fn();
    render(<SignalList signals={signals} empty="no signals" onCorrId={onCorrId} />);

    await user.click(screen.getByRole("button", { name: "corr req-42" }));

    expect(onCorrId).toHaveBeenCalledWith("req-42");
  });

  it("shows the correlation id as plain text when nothing listens for it", () => {
    render(<SignalList signals={signals} empty="no signals" />);

    expect(screen.getByText("corr req-42")).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("shows the empty message when there are no signals", () => {
    render(<SignalList signals={[]} empty="no signals yet" />);

    expect(screen.getByText("no signals yet")).toBeInTheDocument();
    expect(screen.queryByRole("list")).toBeNull();
  });
});
