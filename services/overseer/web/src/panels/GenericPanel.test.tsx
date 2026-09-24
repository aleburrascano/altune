import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { GenericPanel } from "./GenericPanel";
import type { Snapshot, State } from "../types";

function snap<D>(state: State, data: D): Snapshot<D> {
  return {
    id: "x",
    title: "X",
    state,
    severity: "ok",
    headline: "",
    updatedAt: new Date().toISOString(),
    data,
  };
}

describe("GenericPanel key/value view", () => {
  it("never renders a <pre> element", () => {
    const { container } = render(<GenericPanel snapshot={snap("live", { a: 1 })} />);
    expect(container.querySelector("pre")).toBeNull();
  });

  it("renders a nested object as a key/value list", () => {
    render(
      <GenericPanel
        snapshot={snap("live", { region: "us-east", limits: { max: 10, window: { unit: "s" } } })}
      />,
    );
    expect(screen.getByText("region")).toBeInTheDocument();
    expect(screen.getByText("us-east")).toBeInTheDocument();
    expect(screen.getByText("limits")).toBeInTheDocument();
    expect(screen.getByText("max")).toBeInTheDocument();
    expect(screen.getByText("10")).toBeInTheDocument();
  });

  it("summarises an array instead of dumping every element", () => {
    render(<GenericPanel snapshot={snap("live", { tags: ["a", "b", "c", "d", "e"] })} />);
    expect(screen.getByText(/\+2 more \(5\)/)).toBeInTheDocument();
  });

  it("keeps deeply nested watched-app text as escaped text, never as markup", () => {
    const { container } = render(
      <GenericPanel
        snapshot={snap("live", { level1: { level2: { note: "<script>alert(1)</script>" } } })}
      />,
    );
    expect(container.querySelector("script")).toBeNull();
    expect(container.textContent).toContain("<script>alert(1)</script>");
  });
});
