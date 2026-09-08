import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { SQLSchemaBrowser } from "./sql-schema-browser";

const table = {
  schema: "public",
  table: "users",
  columnCount: 2,
  columns: [
    { name: "id", dataType: "integer", position: 1 },
    { name: "email", dataType: "text", position: 2 },
  ],
};

function renderBrowser(overrides = {}) {
  const props = {
    rows: [table],
    search: "",
    onSearch: vi.fn(),
    onPrepareQuery: vi.fn(),
    metadata: { state: "ready", tables: [table], error: "", truncated: false },
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
