import { describe, it, expect } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Metric, StatGrid } from "./index";

describe("StatGrid and Metric", () => {
  it("shows the value, unit and label, colored by tone", () => {
    render(
      <StatGrid>
        <Metric label="Error rate" value={4.2} unit="%" tone="critical" />
        <Metric label="Requests" value="1,204" />
      </StatGrid>,
    );

    expect(screen.getByText("4.2")).toHaveClass("text-critical");
    expect(screen.getByText("%")).toBeInTheDocument();
    expect(screen.getByText("Error rate")).toBeInTheDocument();
    expect(screen.getByText("1,204")).toHaveClass("text-fg");
  });

  it("offers no help button without a hint", () => {
    render(<Metric label="Requests" value={3} />);

    expect(screen.queryByRole("button")).toBeNull();
  });

  it("describes its help button with the hint once keyboard focus opens the tooltip", async () => {
    const user = userEvent.setup();
    render(<Metric label="p95" value={120} unit="ms" hint="95th percentile latency over the window" />);

    await user.tab();

    const help = screen.getByRole("button", { name: "About p95" });
    expect(help).toHaveFocus();
    await waitFor(() => expect(help).toHaveAccessibleDescription("95th percentile latency over the window"));
  });
});

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
