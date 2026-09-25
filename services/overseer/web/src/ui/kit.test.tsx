import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Metric, Notice, Panel, Section, SectionTabs, SignalList, StatGrid, type NoticeKind, type Signal } from "./index";
import type { Snapshot } from "../types";

function snapshot(overrides: Partial<Snapshot> = {}): Snapshot {
  return {
    id: "reliability",
    title: "Reliability",
    state: "stale",
    reason: "auth",
    severity: "critical",
    headline: "99.2%",
    updatedAt: new Date().toISOString(),
    data: {},
    ...overrides,
  };
}

describe("Panel", () => {
  it("heads the body with its title, state badge and age, and names the bucket in the footer", () => {
    render(
      <Panel title="Reliability" snapshot={snapshot()} actions={<button type="button">Refresh</button>}>
        <p>body</p>
      </Panel>,
    );

    const panel = screen.getByRole("article", { name: "Reliability" });
    expect(panel).toHaveTextContent("STALE");
    expect(panel).toHaveTextContent("login problem");
    expect(panel).toHaveTextContent("0s ago");
    expect(screen.getByRole("button", { name: "Refresh" })).toBeInTheDocument();
    expect(screen.getByText("body")).toBeInTheDocument();
    expect(screen.getByRole("contentinfo")).toHaveTextContent("reliability");
  });
});

describe("Section", () => {
  it("labels its region with its title", () => {
    render(
      <Section title="Latency">
        <p>chart</p>
      </Section>,
    );

    expect(screen.getByRole("region", { name: "Latency" })).toHaveTextContent("chart");
  });
});

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

describe("Notice", () => {
  it.each<[NoticeKind, string]>([
    ["stale", "text-warn"],
    ["down", "text-critical"],
    ["empty", "text-fg-faint"],
    ["lossy", "text-warn"],
  ])("renders a %s notice in its tone", (kind, tone) => {
    render(<Notice kind={kind}>window is lossy</Notice>);

    expect(screen.getByRole("status")).toHaveTextContent("window is lossy");
    expect(screen.getByRole("status")).toHaveClass(tone);
  });
});

describe("SectionTabs", () => {
  it("shows the first section and switches on a tab choice", async () => {
    const user = userEvent.setup();
    render(
      <SectionTabs
        label="Reliability views"
        sections={[
          { title: "Charts", content: <p>charts body</p> },
          { title: "Signals", content: <p>signals body</p> },
        ]}
      />,
    );

    expect(screen.getByText("charts body")).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "Signals" }));

    expect(screen.getByRole("tab", { name: "Signals" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByText("signals body")).toBeInTheDocument();
    expect(screen.queryByText("charts body")).toBeNull();
  });
});
