import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SectionTabs } from "./index";

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
