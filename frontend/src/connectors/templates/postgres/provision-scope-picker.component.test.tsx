import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import { ProvisionScopePicker } from "./provision-scope-picker";
import { buildProvisionScope, buildProvisionSQLPreview } from "./provisioning";
import type { MetadataSchema, ProvisionForm, ScopeSelection } from "./provisioning-types";

function Picker({
  preset = "read_only",
  schemas = [{ name: "public", tables: [{ name: "users", columns: ["id", "email"] }] }],
}: {
  preset?: ProvisionForm["preset"];
  schemas?: MetadataSchema[];
}) {
  const [scope, setScope] = useState<ScopeSelection>({ all_schemas: false, schemas: {} });
  const selected = buildProvisionScope(scope);
  return (
    <>
      <ProvisionScopePicker metadata={{ state: "ready", schemas }} scope={scope} onChange={setScope} preset={preset} />
      <output aria-label="Selected scope">{JSON.stringify(selected)}</output>
      <output aria-label="SQL preview">
        {buildProvisionSQLPreview({ roleName: "reader", database: "appdb", preset, scope: selected })}
      </output>
    </>
  );
}

it("narrows schema, table and column selections using functional state updates", async () => {
  const user = userEvent.setup();
  render(<Picker />);
  await user.click(screen.getByRole("checkbox", { name: "Select schema public" }));
  await user.click(screen.getByRole("checkbox", { name: "All tables and columns in this schema" }));
  await user.click(screen.getByRole("checkbox", { name: "Select table users" }));
  await user.click(screen.getByRole("checkbox", { name: "All columns" }));
  await user.click(screen.getByRole("checkbox", { name: "email" }));
  expect(screen.getByRole("checkbox", { name: "email" })).toBeChecked();
  expect(screen.getByRole("checkbox", { name: "id" })).not.toBeChecked();
  await user.click(screen.getByRole("checkbox", { name: "Select schema public" }));
  expect(screen.queryByRole("checkbox", { name: "Select table users" })).not.toBeInTheDocument();
});

it("requires all columns for the write preset", async () => {
  const user = userEvent.setup();
  render(<Picker preset="read_write" />);
  await user.click(screen.getByRole("checkbox", { name: "Select schema public" }));
  await user.click(screen.getByRole("checkbox", { name: "All tables and columns in this schema" }));
  await user.click(screen.getByRole("checkbox", { name: "Select table users" }));
  const allColumns = screen.getByRole("checkbox", { name: "All columns required for read and change preset" });
  expect(allColumns).toBeDisabled();
  expect(allColumns).toBeChecked();
  expect(screen.queryByRole("checkbox", { name: "email" })).not.toBeInTheDocument();
});

it("distinguishes loading metadata from an empty scope", () => {
  const scope: ScopeSelection = { all_schemas: false, schemas: {} };
  const { rerender } = render(
    <ProvisionScopePicker metadata={{ state: "loading", schemas: [] }} scope={scope} onChange={() => {}} preset="read_only" />,
  );
  expect(screen.getByText("Loading schema metadata...")).toBeVisible();
  rerender(<ProvisionScopePicker metadata={{ state: "ready", schemas: [] }} scope={scope} onChange={() => {}} preset="read_only" />);
  expect(screen.getByText(/No schema metadata loaded/)).toBeVisible();
});

it.each(["__proto__", "constructor", "toString"])("selects %s resources only on explicit clicks", async (name) => {
  const user = userEvent.setup();
  render(<Picker schemas={[{ name, tables: [{ name, columns: [name] }] }]} />);
  const schema = screen.getByRole("checkbox", { name: `Select schema ${name}` });
  expect(schema).not.toBeChecked();
  await user.click(schema);
  expect(schema).toBeChecked();
  await user.click(screen.getByRole("checkbox", { name: "All tables and columns in this schema" }));
  const table = screen.getByRole("checkbox", { name: `Select table ${name}` });
  expect(table).not.toBeChecked();
  await user.click(table);
  expect(table).toBeChecked();
  await user.click(screen.getByRole("checkbox", { name: "All columns" }));
  const column = screen.getByRole("checkbox", { name });
  expect(column).not.toBeChecked();
  expect(screen.getByLabelText("Selected scope")).toHaveTextContent("null");
  await user.click(column);
  expect(column).toBeChecked();
  expect(JSON.parse(screen.getByLabelText("Selected scope").textContent || "null")).toEqual({
    all_schemas: false,
    schemas: [{ schema: name, all_tables: false, tables: [{ table: name, all_columns: false, columns: [name] }] }],
  });
  expect(screen.getByLabelText("SQL preview")).toHaveTextContent(`GRANT SELECT ("${name}") ON TABLE "${name}"."${name}" TO "reader";`);
  await user.click(column);
  expect(column).not.toBeChecked();
  expect(screen.getByLabelText("Selected scope")).toHaveTextContent("null");
});
