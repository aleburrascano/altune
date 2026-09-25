import { describe, it, expect } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { DataTable, type Column } from "./index";

interface Route {
  route: string;
  p95: number | null;
  status: string;
}

const columns: Column<Route>[] = [
  { key: "route", label: "Route", sortable: true },
  { key: "p95", label: "p95", align: "right", sortable: true },
  { key: "status", label: "Status" },
];

const rows: Route[] = [
  { route: "/v1/tracks", p95: 120, status: "ok" },
  { route: "/v1/search", p95: null, status: "ok" },
  { route: "/v1/albums", p95: 9, status: "slow" },
  { route: "/v1/artists", p95: 45, status: "ok" },
];

function routeColumn(): string[] {
  return screen
    .getAllByRole("row")
    .slice(1)
    .map((row) => within(row).getAllByRole("cell")[0].textContent ?? "");
}

describe("DataTable", () => {
  it("renders rows in their given order until a header is chosen", () => {
    render(<DataTable columns={columns} rows={rows} empty="no routes" />);

    expect(routeColumn()).toEqual(["/v1/tracks", "/v1/search", "/v1/albums", "/v1/artists"]);
    expect(screen.getByRole("columnheader", { name: "p95" })).toHaveAttribute("aria-sort", "none");
    expect(screen.getByRole("columnheader", { name: "Status" })).not.toHaveAttribute("aria-sort");
    expect(screen.getAllByText("—")).toHaveLength(1);
  });

  it("sorts ascending on a header click, then descending on the next, missing values last", async () => {
    const user = userEvent.setup();
    render(<DataTable columns={columns} rows={rows} empty="no routes" />);
    const p95 = screen.getByRole("button", { name: "p95" });

    await user.click(p95);
    const ascending = routeColumn();
    await user.click(p95);

    expect(ascending).toEqual(["/v1/albums", "/v1/artists", "/v1/tracks", "/v1/search"]);
    expect(routeColumn()).toEqual(["/v1/tracks", "/v1/artists", "/v1/albums", "/v1/search"]);
    expect(screen.getByRole("columnheader", { name: /p95/ })).toHaveAttribute("aria-sort", "descending");
  });

  it("sorts from the keyboard", async () => {
    const user = userEvent.setup();
    render(<DataTable columns={columns} rows={rows} empty="no routes" />);

    await user.tab();
    await user.keyboard("{Enter}");

    expect(screen.getByRole("button", { name: /Route/ })).toHaveFocus();
    expect(routeColumn()).toEqual(["/v1/albums", "/v1/artists", "/v1/search", "/v1/tracks"]);
    expect(screen.getByRole("columnheader", { name: /Route/ })).toHaveAttribute("aria-sort", "ascending");
  });

  it("shows the empty message instead of a bare header when there are no rows", () => {
    render(<DataTable columns={columns} rows={[]} empty="no routes yet" />);

    expect(screen.getByText("no routes yet")).toBeInTheDocument();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("renders hostile cell text as text", () => {
    const { container } = render(
      <DataTable
        columns={columns}
        rows={[{ route: "<img src=x onerror=alert(1)>", p95: 1, status: "ok" }]}
        empty="no routes"
      />,
    );

    expect(screen.getByText("<img src=x onerror=alert(1)>")).toBeInTheDocument();
    expect(container.querySelector("img")).toBeNull();
  });
});

describe("custom cell render", () => {
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
});
