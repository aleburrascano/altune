import { useRef, useState } from "react";
import { describe, it, expect } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useParams } from "react-router-dom";
import { CommandPalette } from "./CommandPalette";
import type { Snapshot } from "../types";

function snapshot(overrides: Partial<Snapshot> = {}): Snapshot {
  return {
    id: "reliability",
    title: "Reliability",
    state: "live",
    severity: "ok",
    headline: "100%",
    updatedAt: new Date().toISOString(),
    data: {},
    ...overrides,
  };
}

const buckets = [
  snapshot({ id: "reliability", title: "Reliability" }),
  snapshot({ id: "cost", title: "Cost" }),
];

function BucketPage() {
  const { id } = useParams<{ id: string }>();
  return <p>on bucket page: {id}</p>;
}

function Harness() {
  const [open, setOpen] = useState(false);
  const openerRef = useRef<HTMLElement | null>(null);
  return (
    <>
      <button
        type="button"
        onClick={(event) => {
          openerRef.current = event.currentTarget;
          setOpen(true);
        }}
      >
        Open palette
      </button>
      <CommandPalette
        buckets={buckets}
        open={open}
        onOpenChange={setOpen}
        restoreFocusTo={() => openerRef.current?.focus()}
      />
      <Routes>
        <Route path="/bucket/:id" element={<BucketPage />} />
        <Route path="/" element={<p>overview page</p>} />
      </Routes>
    </>
  );
}

function harness() {
  return render(
    <MemoryRouter initialEntries={["/"]}>
      <Harness />
    </MemoryRouter>,
  );
}

describe("CommandPalette", () => {
  it("filters the list as the query changes", async () => {
    const user = userEvent.setup();
    harness();
    await user.click(screen.getByRole("button", { name: "Open palette" }));

    await user.type(screen.getByRole("combobox", { name: "Jump to a bucket" }), "cost");

    expect(screen.getByRole("option", { name: "Cost" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "Reliability" })).not.toBeInTheDocument();
  });

  it("navigates to the first match on Enter and closes the palette", async () => {
    const user = userEvent.setup();
    harness();
    await user.click(screen.getByRole("button", { name: "Open palette" }));

    await user.type(screen.getByRole("combobox", { name: "Jump to a bucket" }), "cost{Enter}");

    await waitFor(() => expect(screen.getByText("on bucket page: cost")).toBeInTheDocument());
    expect(screen.queryByRole("combobox", { name: "Jump to a bucket" })).not.toBeInTheDocument();
  });

  it("closes on Escape and returns focus to the trigger", async () => {
    const user = userEvent.setup();
    harness();
    const trigger = screen.getByRole("button", { name: "Open palette" });
    await user.click(trigger);

    await user.keyboard("{Escape}");

    await waitFor(() => expect(screen.queryByRole("combobox", { name: "Jump to a bucket" })).not.toBeInTheDocument());
    await waitFor(() => expect(trigger).toHaveFocus());
  });

  it("exposes combobox semantics wired to the listbox and the active option", async () => {
    const user = userEvent.setup();
    harness();
    await user.click(screen.getByRole("button", { name: "Open palette" }));

    const input = screen.getByRole("combobox", { name: "Jump to a bucket" });
    const listbox = screen.getByRole("listbox");
    const firstOption = screen.getByRole("option", { name: "Reliability" });

    expect(input).toHaveAttribute("aria-expanded", "true");
    expect(input).toHaveAttribute("aria-controls", listbox.id);
    expect(input).toHaveAttribute("aria-activedescendant", firstOption.id);
    expect(firstOption).toHaveAttribute("aria-selected", "true");
  });

  it("moves the active option down with ArrowDown and wraps past the last option", async () => {
    const user = userEvent.setup();
    harness();
    await user.click(screen.getByRole("button", { name: "Open palette" }));

    const input = screen.getByRole("combobox", { name: "Jump to a bucket" });
    const reliability = screen.getByRole("option", { name: "Reliability" });
    const cost = screen.getByRole("option", { name: "Cost" });

    await user.type(input, "{ArrowDown}");
    expect(cost).toHaveAttribute("aria-selected", "true");
    expect(input).toHaveAttribute("aria-activedescendant", cost.id);

    await user.type(input, "{ArrowDown}");
    expect(reliability).toHaveAttribute("aria-selected", "true");
  });

  it("moves the active option up with ArrowUp and wraps past the first option", async () => {
    const user = userEvent.setup();
    harness();
    await user.click(screen.getByRole("button", { name: "Open palette" }));

    const input = screen.getByRole("combobox", { name: "Jump to a bucket" });
    const cost = screen.getByRole("option", { name: "Cost" });

    await user.type(input, "{ArrowUp}");

    expect(cost).toHaveAttribute("aria-selected", "true");
  });

  it("jumps to the first and last option with Home and End", async () => {
    const user = userEvent.setup();
    harness();
    await user.click(screen.getByRole("button", { name: "Open palette" }));

    const input = screen.getByRole("combobox", { name: "Jump to a bucket" });
    const reliability = screen.getByRole("option", { name: "Reliability" });
    const cost = screen.getByRole("option", { name: "Cost" });

    await user.type(input, "{End}");
    expect(cost).toHaveAttribute("aria-selected", "true");

    await user.type(input, "{Home}");
    expect(reliability).toHaveAttribute("aria-selected", "true");
  });

  it("navigates to the active option on Enter, not always the first", async () => {
    const user = userEvent.setup();
    harness();
    await user.click(screen.getByRole("button", { name: "Open palette" }));

    const input = screen.getByRole("combobox", { name: "Jump to a bucket" });
    await user.type(input, "{ArrowDown}{Enter}");

    await waitFor(() => expect(screen.getByText("on bucket page: cost")).toBeInTheDocument());
  });

  it("resets the active option to the first when the query changes", async () => {
    const user = userEvent.setup();
    harness();
    await user.click(screen.getByRole("button", { name: "Open palette" }));

    const input = screen.getByRole("combobox", { name: "Jump to a bucket" });
    await user.type(input, "{ArrowDown}");
    expect(screen.getByRole("option", { name: "Cost" })).toHaveAttribute("aria-selected", "true");

    await user.type(input, "t");

    const reliability = screen.getByRole("option", { name: "Reliability" });
    const cost = screen.getByRole("option", { name: "Cost" });
    expect(reliability).toHaveAttribute("aria-selected", "true");
    expect(cost).toHaveAttribute("aria-selected", "false");
    expect(input).toHaveAttribute("aria-activedescendant", reliability.id);
  });

  it("keeps Tab focus inside the open palette", async () => {
    const user = userEvent.setup();
    harness();
    await user.click(screen.getByRole("button", { name: "Open palette" }));

    const input = screen.getByRole("combobox", { name: "Jump to a bucket" });
    expect(input).toHaveFocus();

    await user.tab();

    expect(screen.getByRole("dialog")).toContainElement(document.activeElement as HTMLElement);
  });
});
