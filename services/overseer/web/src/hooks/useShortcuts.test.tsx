import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useShortcuts } from "./useShortcuts";

function Harness({
  openPalette,
  nextBucket,
  previousBucket,
}: {
  openPalette: () => void;
  nextBucket: () => void;
  previousBucket: () => void;
}) {
  useShortcuts({ openPalette, nextBucket, previousBucket });
  return <input aria-label="somewhere else" />;
}

describe("useShortcuts", () => {
  it("opens the palette on Ctrl+K", async () => {
    const openPalette = vi.fn();
    const user = userEvent.setup();
    render(<Harness openPalette={openPalette} nextBucket={vi.fn()} previousBucket={vi.fn()} />);

    await user.keyboard("{Control>}k{/Control}");

    expect(openPalette).toHaveBeenCalledTimes(1);
  });

  it("moves to the next bucket on j and the previous bucket on k", async () => {
    const nextBucket = vi.fn();
    const previousBucket = vi.fn();
    const user = userEvent.setup();
    render(<Harness openPalette={vi.fn()} nextBucket={nextBucket} previousBucket={previousBucket} />);

    await user.keyboard("j");
    await user.keyboard("k");

    expect(nextBucket).toHaveBeenCalledTimes(1);
    expect(previousBucket).toHaveBeenCalledTimes(1);
  });

  it("ignores j and k while typing in an input", async () => {
    const nextBucket = vi.fn();
    const previousBucket = vi.fn();
    const user = userEvent.setup();
    render(<Harness openPalette={vi.fn()} nextBucket={nextBucket} previousBucket={previousBucket} />);

    await user.click(screen.getByLabelText("somewhere else"));
    await user.keyboard("jk");

    expect(nextBucket).not.toHaveBeenCalled();
    expect(previousBucket).not.toHaveBeenCalled();
  });

  it("leaves a browser shortcut that happens to share a modifier and letter alone", async () => {
    const nextBucket = vi.fn();
    const user = userEvent.setup();
    render(<Harness openPalette={vi.fn()} nextBucket={nextBucket} previousBucket={vi.fn()} />);

    await user.keyboard("{Control>}j{/Control}");

    expect(nextBucket).not.toHaveBeenCalled();
  });

  it("does not open the palette on Shift+Cmd+K", async () => {
    const openPalette = vi.fn();
    const user = userEvent.setup();
    render(<Harness openPalette={openPalette} nextBucket={vi.fn()} previousBucket={vi.fn()} />);

    await user.keyboard("{Shift>}{Meta>}k{/Meta}{/Shift}");

    expect(openPalette).not.toHaveBeenCalled();
  });

  it("does not move to the next bucket on Shift+J", async () => {
    const nextBucket = vi.fn();
    const user = userEvent.setup();
    render(<Harness openPalette={vi.fn()} nextBucket={nextBucket} previousBucket={vi.fn()} />);

    await user.keyboard("{Shift>}j{/Shift}");

    expect(nextBucket).not.toHaveBeenCalled();
  });

  it("does not move to the next bucket on Alt+J", async () => {
    const nextBucket = vi.fn();
    const user = userEvent.setup();
    render(<Harness openPalette={vi.fn()} nextBucket={nextBucket} previousBucket={vi.fn()} />);

    await user.keyboard("{Alt>}j{/Alt}");

    expect(nextBucket).not.toHaveBeenCalled();
  });
});
