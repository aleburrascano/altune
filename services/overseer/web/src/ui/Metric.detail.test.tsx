import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { Metric } from "./index";

describe("Metric detail", () => {
  it("renders the detail line always visible, with no tooltip needed", () => {
    render(<Metric label="eval score" value="0.87" detail="ran 6d ago · stale" />);

    expect(screen.getByText("ran 6d ago · stale")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "About eval score" })).toBeNull();
  });

  it("keeps existing callers without a detail unaffected", () => {
    render(<Metric label="uptime" value="99%" />);

    expect(screen.getByText("99%")).toBeInTheDocument();
    expect(screen.getByText("uptime")).toBeInTheDocument();
  });

  it("shows both the detail line and the tooltip hint when both are given", async () => {
    render(<Metric label="suspect rate" value="42%" hint="sample 5m ago" detail="sample 5m ago" />);

    expect(screen.getByText("sample 5m ago")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "About suspect rate" })).toBeInTheDocument();
  });

  it("wraps a detail node so it stays visible instead of being clipped", () => {
    render(
      <Metric
        label="eval score"
        value="0.87"
        detail={<span className="text-warn">ran 6d ago · aged, stale</span>}
      />,
    );

    const detail = screen.getByText("ran 6d ago · aged, stale");
    expect(detail).toHaveClass("text-warn");
  });
});
