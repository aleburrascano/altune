import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { Notice, type NoticeKind } from "./index";

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
