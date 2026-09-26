import { expect, it } from "vitest";
import { buildProvisionScope, groupMetadataRows, toggleColumn, toggleSchema, toggleTable, updateSchema, updateTable } from "./provisioning";
import type { ScopeSelection } from "./provisioning-types";

it("rejects malformed metadata rows without losing ordered valid columns", () => {
  expect(groupMetadataRows(null)).toEqual([]);
  expect(groupMetadataRows({ rows: [] })).toEqual([]);
  expect(
    groupMetadataRows([
      null,
      7,
      { table_schema: "public", table_name: "users", columns: "id,email" },
      { table_schema: "public", table_name: "users", column_name: "name" },
      { table_schema: "public", table_name: "users", columns: '["email","created_at"]' },
      { table_schema: "", table_name: "ignored", columns: ["id"] },
    ]),
  ).toEqual([{ name: "public", tables: [{ name: "users", columns: ["id", "email", "name", "created_at"] }] }]);
});

it("updates nested scope immutably and projects only explicitly selected columns", () => {
  const empty: ScopeSelection = { all_schemas: false, schemas: {} };
  const schema = updateSchema(toggleSchema(empty, "public", true), "public", { all_tables: false });
  const table = updateTable(toggleTable(schema, "public", "users", true), "public", "users", { all_columns: false });
  const id = toggleColumn(table, "public", "users", "id", true);
  const email = toggleColumn(id, "public", "users", "email", true);
  const selected = toggleColumn(email, "public", "users", "id", false);

  expect(empty).toEqual({ all_schemas: false, schemas: {} });
  expect(id.schemas.public.tables.users.columns).toEqual({ id: true });
  expect(email.schemas.public.tables.users.columns).toEqual({ id: true, email: true });
  expect(buildProvisionScope(selected)).toEqual({
    all_schemas: false,
    schemas: [{ schema: "public", all_tables: false, tables: [{ table: "users", all_columns: false, columns: ["email"] }] }],
  });
  expect(buildProvisionScope(toggleSchema(selected, "public", false))).toBeNull();
});
