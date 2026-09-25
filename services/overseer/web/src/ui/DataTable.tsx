import type { ReactNode } from "react";
import { useState } from "react";
import { Notice } from "./Notice";
import { focusRing } from "./focusRing";

export type CellValue = string | number | null | undefined;

export type TableRow<T> = { [K in keyof T]: CellValue };

export interface Column<T> {
  key: keyof T;
  label: string;
  align?: "left" | "right";
  sortable?: boolean;
  render?: (value: CellValue, row: T) => ReactNode;
}

type Direction = "ascending" | "descending";

interface SortOrder<T> {
  key: keyof T;
  direction: Direction;
}

const MISSING = "—";

function isMissing(value: CellValue): value is null | undefined {
  return value === null || value === undefined;
}

export function compareCells(left: CellValue, right: CellValue): number {
  if (isMissing(left) || isMissing(right)) return Number(isMissing(left)) - Number(isMissing(right));
  if (typeof left === "number" && typeof right === "number") return left - right;
  return String(left).localeCompare(String(right), undefined, { numeric: true });
}

function sortedRowIndexes<T extends TableRow<T>>(rows: T[], order: SortOrder<T> | null): number[] {
  const indexes = rows.map((_, index) => index);
  if (!order) return indexes;
  const sign = order.direction === "ascending" ? 1 : -1;
  return indexes.sort((a, b) => {
    const left = rows[a][order.key];
    const right = rows[b][order.key];
    if (isMissing(left) || isMissing(right)) return compareCells(left, right);
    return sign * compareCells(left, right);
  });
}

function nextOrder<T>(current: SortOrder<T> | null, key: keyof T): SortOrder<T> {
  const isSameAscending = current?.key === key && current.direction === "ascending";
  return { key, direction: isSameAscending ? "descending" : "ascending" };
}

const ALIGN_CLASSES = { left: "text-left", right: "text-right" } as const;

export function DataTable<T extends TableRow<T>>({
  columns,
  rows,
  empty,
}: {
  columns: Column<T>[];
  rows: T[];
  empty: string;
}) {
  const [order, setOrder] = useState<SortOrder<T> | null>(null);

  if (rows.length === 0) return <Notice kind="empty">{empty}</Notice>;

  return (
    <div className="min-w-0 overflow-x-auto">
      <table className="w-full border-collapse text-sm">
        <thead>
          <tr className="border-b border-border">
            {columns.map((column) => {
              const align = ALIGN_CLASSES[column.align ?? "left"];
              const sortState = order?.key === column.key ? order.direction : "none";
              return (
                <th
                  key={String(column.key)}
                  scope="col"
                  aria-sort={column.sortable ? sortState : undefined}
                  className={`px-2 py-1.5 text-xs font-medium uppercase tracking-wider text-fg-faint ${align}`}
                >
                  {column.sortable ? (
                    <button
                      type="button"
                      onClick={() => setOrder((current) => nextOrder(current, column.key))}
                      className={`inline-flex items-center gap-1 border-0 bg-transparent p-0 uppercase tracking-wider text-inherit hover:text-fg ${focusRing}`}
                    >
                      {column.label}
                      <span aria-hidden="true">{sortGlyph(sortState)}</span>
                    </button>
                  ) : (
                    column.label
                  )}
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {sortedRowIndexes(rows, order).map((rowIndex) => (
            <tr key={rowIndex} className="border-b border-border last:border-b-0 hover:bg-bg-elev-2">
              {columns.map((column) => {
                const value = rows[rowIndex][column.key];
                const row = rows[rowIndex];
                return (
                  <td
                    key={String(column.key)}
                    className={`px-2 py-1.5 text-fg ${ALIGN_CLASSES[column.align ?? "left"]} ${typeof value === "number" ? "font-mono" : ""}`}
                  >
                    {column.render ? column.render(value, row) : isMissing(value) ? MISSING : value}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function sortGlyph(sortState: Direction | "none"): string {
  if (sortState === "ascending") return "▲";
  if (sortState === "descending") return "▼";
  return "";
}
