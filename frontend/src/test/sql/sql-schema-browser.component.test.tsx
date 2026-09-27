import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { SQLSchemaBrowser } from "../../connectors/templates/_shared/sql-schema-browser";
import type { SQLSchemaBrowserProps } from "../../connectors/templates/_shared/sql-schema-browser";
import type { BrowserTable } from "../../connectors/templates/_shared/sql-console-config";

const table: BrowserTable = {
  schema: "public",
  table: "users",
  columnCount: 2,
  type: "table",
  columns: [
    { name: "id", dataType: "integer", position: 1 },
    { name: "email", dataType: "text", position: 2 },
  ],
};

function renderBrowser(overrides: Partial<SQLSchemaBrowserProps> = {}) {
  const props: SQLSchemaBrowserProps = {
    rows: [table],
    search: "",
    onSearch: vi.fn(),
    onPrepareQuery: vi.fn(),
    metadata: { state: "ready", error: "" },
    theme: "dark",
    inputClass: "",
    mutedClass: "",
    hoverClass: "",
    namespaceLabel: "Schema",
    ...overrides,
  };
  return { ...render(<SQLSchemaBrowser {...props} />), props };
}

it("searches, expands table columns, and prepares a query accessibly", async () => {
  const user = userEvent.setup();
  const { props } = renderBrowser();

  await user.type(screen.getByRole("searchbox", { name: "Search schemas or tables" }), "user");
  expect(props.onSearch).toHaveBeenLastCalledWith("r");

  const toggle = screen.getByTitle("Show columns for public.users");
  expect(toggle).toHaveAttribute("aria-expanded", "false");
  await user.click(toggle);
  expect(toggle).toHaveAttribute("aria-expanded", "true");
  expect(screen.getByText("email")).toBeVisible();

  await user.click(screen.getByRole("button", { name: "Prepare SELECT query for public.users" }));
  expect(props.onPrepareQuery).toHaveBeenCalledWith(table);
});

it("keeps same-named tables in different schemas independently expanded", async () => {
  const user = userEvent.setup();
  renderBrowser({ rows: [table, { ...table, schema: "reporting", columns: [{ name: "report_id", dataType: "uuid", position: 1 }] }] });

  await user.click(screen.getByTitle("Show columns for reporting.users"));

  expect(screen.getByText("report_id")).toBeVisible();
  expect(screen.queryByText("email")).not.toBeInTheDocument();
  expect(screen.getByTitle("Show columns for public.users")).toHaveAttribute("aria-expanded", "false");
  await user.click(screen.getByTitle("Hide columns for reporting.users"));
  expect(screen.queryByText("report_id")).not.toBeInTheDocument();
});

it("shows loading without an empty-results warning", () => {
  renderBrowser({ rows: [], metadata: { state: "loading", error: "" } });

  expect(screen.getByText("Loading schema metadata...")).toBeVisible();
  expect(screen.queryByText("No tables found for this profile.")).not.toBeInTheDocument();
});

it("reports unavailable metadata and supports tables without column details", async () => {
  const user = userEvent.setup();
  renderBrowser({ rows: [{ ...table, columns: [], columnCount: 0 }], metadata: { state: "error", error: "Metadata permission denied" } });

  expect(screen.getByText("Metadata permission denied")).toBeVisible();
  await user.click(screen.getByTitle("Show columns for public.users"));
  expect(screen.getByText("No column metadata loaded.")).toBeVisible();
});
