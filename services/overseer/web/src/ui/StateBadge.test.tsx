import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { StateBadge } from "./index";
import type { Reason, Severity, State } from "../types";

describe("StateBadge", () => {
  it.each<[Reason, string]>([
    ["auth", "login problem"],
    ["throttled", "throttled"],
    ["degraded", "degraded"],
    ["down", "unreachable"],
    ["connecting", "reconnecting"],
  ])("names the %s reason as %s", (reason, text) => {
    render(<StateBadge state="stale" severity="warn" reason={reason} />);

    expect(screen.getByText(text)).toBeInTheDocument();
    expect(screen.getByText("STALE")).toBeInTheDocument();
  });

  it.each<[State, string]>([
    ["live", "LIVE"],
    ["stale", "STALE"],
    ["source_down", "SOURCE DOWN"],
  ])("labels the %s state as %s", (state, label) => {
    render(<StateBadge state={state} severity="ok" />);

    expect(screen.getByText(label)).toBeInTheDocument();
  });

  it.each<Severity>(["ok", "warn", "critical"])("announces and colors the %s severity", (severity) => {
    const { container } = render(<StateBadge state="live" severity={severity} />);

    expect(screen.getByText(`severity ${severity}`)).toHaveClass("sr-only");
    expect(container.firstElementChild).toHaveClass(`text-${severity}`);
  });

  it("shows no reason text when the envelope carries none", () => {
    const { container } = render(<StateBadge state="live" severity="ok" />);

    expect(container.textContent).toBe("LIVEseverity ok");
  });
});
