import { describe, expect, it } from "vitest";
import { fireEvent, render } from "@testing-library/react";
import { LogTail, type LogRow } from "./LogTail";

const ROW_HEIGHT = 24;

function makeRows(count: number, prefix = "row"): LogRow[] {
  return Array.from({ length: count }, (_, i) => ({
    time: `2026-09-15T10:00:${String(i % 60).padStart(2, "0")}Z`,
    level: "INFO",
    msg: `${prefix}-${i}`,
    attrs: null,
  }));
}

function indexOf(rows: LogRow[], li: Element): number {
  const match = li.textContent?.match(/(?:row|next)-(\d+)/);
  expect(match).not.toBeNull();
  const found = rows.findIndex((row) => row.msg === match![0]);
  expect(found).toBeGreaterThanOrEqual(0);
  return found;
}

function assertRowsPositioned(container: HTMLElement, rows: LogRow[]) {
  const items = Array.from(container.querySelectorAll("li"));
  expect(items.length).toBeGreaterThan(0);
  for (const li of items) {
    const index = indexOf(rows, li);
    const element = li as HTMLLIElement;
    expect(element.style.height).toBe(`${ROW_HEIGHT}px`);
    expect(element.style.transform).toBe(`translateY(${index * ROW_HEIGHT}px)`);
  }
}

describe("LogTail row positioning", () => {
  it("positions each rendered row at its virtualizer offset and size", () => {
    const rows = makeRows(100);
    const { container } = render(<LogTail rows={rows} empty="none" />);
    assertRowsPositioned(container, rows);
  });

  it("repositions rows after the rows array changes", () => {
    const rows = makeRows(100);
    const { container, rerender } = render(<LogTail rows={rows} empty="none" />);
    assertRowsPositioned(container, rows);

    const nextRows = makeRows(150, "next");
    rerender(<LogTail rows={nextRows} empty="none" />);
    assertRowsPositioned(container, nextRows);
  });

  it("repositions rows after a scroll, once the next frame runs", async () => {
    const rows = makeRows(200);
    const { container } = render(<LogTail rows={rows} empty="none" />);
    const scroller = container.querySelector<HTMLDivElement>("div")!;

    scroller.scrollTop = 500;
    fireEvent.scroll(scroller);
    await new Promise((resolve) => requestAnimationFrame(resolve));

    assertRowsPositioned(container, rows);
    const items = Array.from(container.querySelectorAll("li"));
    const firstIndex = indexOf(rows, items[0]);
    expect(firstIndex).toBeGreaterThan(0);
  });
});
