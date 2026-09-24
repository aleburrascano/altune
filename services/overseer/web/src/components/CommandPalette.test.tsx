import { useRef, useState } from "react";
import { describe, it, expect } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
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
        <Route path="/bucket/:id" element={<p>on bucket page</p>} />
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

    await user.type(screen.getByRole("textbox", { name: "Jump to a bucket" }), "cost");

    expect(screen.getByRole("option", { name: "Cost" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "Reliability" })).not.toBeInTheDocument();
  });

  it("navigates to the first match on Enter and closes the palette", async () => {
    const user = userEvent.setup();
    harness();
    await user.click(screen.getByRole("button", { name: "Open palette" }));

    await user.type(screen.getByRole("textbox", { name: "Jump to a bucket" }), "cost{Enter}");

    await waitFor(() => expect(screen.getByText("on bucket page")).toBeInTheDocument());
    expect(screen.queryByRole("textbox", { name: "Jump to a bucket" })).not.toBeInTheDocument();
  });

  it("closes on Escape and returns focus to the trigger", async () => {
    const user = userEvent.setup();
    harness();
    const trigger = screen.getByRole("button", { name: "Open palette" });
    await user.click(trigger);

    await user.keyboard("{Escape}");

    await waitFor(() => expect(screen.queryByRole("textbox", { name: "Jump to a bucket" })).not.toBeInTheDocument());
    await waitFor(() => expect(trigger).toHaveFocus());
  });
});
