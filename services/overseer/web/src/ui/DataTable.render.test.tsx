import { describe, it, expect } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { DataTable, type Column } from "./index";

interface Route {
  route: string;
  p95: number;
}

const columns: Column<Route>[] = [
  { key: "route", label: "Route", sortable: true },
  {
    key: "p95",
    label: "p95",
    align: "right",
    sortable: true,
    render: (value) => <span data-testid="p95-cell">{`${value} ms`}</span>,
  },
];

const rows: Route[] = [
  { route: "/v1/tracks", p95: 120 },
  { route: "/v1/albums", p95: 9 },
  { route: "/v1/artists", p95: 45 },
];

function routeColumn(): string[] {
  return screen
    .getAllByRole("row")
    .slice(1)
    .map((row) => within(row).getAllByRole("cell")[0].textContent ?? "");
}

describe("DataTable render", () => {
  it("renders a custom cell via the column's render function", () => {
    render(<DataTable columns={columns} rows={rows} empty="no routes" />);

    const cells = screen.getAllByTestId("p95-cell");
    expect(cells.map((cell) => cell.textContent)).toEqual(["120 ms", "9 ms", "45 ms"]);
  });

  it("still sorts on the raw value, not the rendered node", async () => {
    const user = userEvent.setup();
    render(<DataTable columns={columns} rows={rows} empty="no routes" />);

    await user.click(screen.getByRole("button", { name: "p95" }));

    expect(routeColumn()).toEqual(["/v1/albums", "/v1/artists", "/v1/tracks"]);
  });
});
