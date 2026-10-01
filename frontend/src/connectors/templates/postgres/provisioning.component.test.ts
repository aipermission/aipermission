import { expect, it } from "vitest";
import {
  buildProvisionScope,
  buildProvisionSQLPreview,
  groupMetadataRows,
  toggleColumn,
  toggleSchema,
  toggleTable,
  updateSchema,
  updateTable,
} from "./provisioning";
import type { ScopeSelection } from "./provisioning-types";

it("rejects malformed metadata rows without losing ordered valid columns", () => {
  expect(groupMetadataRows(null)).toEqual([]);
  expect(groupMetadataRows({ rows: [] })).toEqual([]);
  expect(
    groupMetadataRows([
      null,
      7,
      { table_schema: "public", table_name: "users", columns: ["id", "email"] },
      { table_schema: "public", table_name: "users", column_name: "name" },
      { table_schema: "public", table_name: "users", columns: '["email","created_at"]' },
      { table_schema: "", table_name: "ignored", columns: ["id"] },
    ]),
  ).toEqual([{ name: "public", tables: [{ name: "users", columns: ["id", "email", "name", "created_at"] }] }]);
});

it.each([
  [" public ", " users ", " id "],
  ['s";--', 'table"name', "column,name"],
  ["__proto__", "constructor", "toString"],
  [" ", "\n", "\t"],
])("preserves exact metadata and grant selection for %j/%j/%j", (schemaName, tableName, columnName) => {
  expect(
    groupMetadataRows([
      { table_schema: schemaName, table_name: tableName, columns: JSON.stringify([columnName]) },
      {
        table_schema: schemaName.trim() === schemaName ? "other" : schemaName.trim(),
        table_name: tableName.trim(),
        columns: ["different"],
      },
    ])[0],
  ).toEqual({ name: schemaName, tables: [{ name: tableName, columns: [columnName] }] });
  let selection: ScopeSelection = { all_schemas: false, schemas: {} };
  selection = updateSchema(toggleSchema(selection, schemaName, true), schemaName, { all_tables: false });
  selection = updateTable(toggleTable(selection, schemaName, tableName, true), schemaName, tableName, { all_columns: false });
  selection = toggleColumn(selection, schemaName, tableName, columnName, true);
  const scope = buildProvisionScope(selection);
  expect(scope).toEqual({
    all_schemas: false,
    schemas: [{ schema: schemaName, all_tables: false, tables: [{ table: tableName, all_columns: false, columns: [columnName] }] }],
  });
  const quoted = (name: string) => `"${name.replaceAll('"', '""')}"`;
  expect(buildProvisionSQLPreview({ roleName: "reader", preset: "read_only", database: "appdb", scope })).toContain(
    `GRANT SELECT (${quoted(columnName)}) ON TABLE ${quoted(schemaName)}.${quoted(tableName)} TO "reader";`,
  );
});

it("does not coerce non-string metadata or split a malformed column payload into different identities", () => {
  expect(
    groupMetadataRows([
      { table_schema: {}, table_name: "users", columns: ["id"] },
      { table_schema: "public", table_name: 7, columns: ["id"] },
      { table_schema: "bad\0schema", table_name: "users", columns: ["id"] },
      { table_schema: "public", table_name: "users", columns: "column,name" },
      { table_schema: "public", table_name: "users", columns: [7, {}, null, "a\0b", " id "] },
    ]),
  ).toEqual([{ name: "public", tables: [{ name: "users", columns: [" id "] }] }]);
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

it("rejects unpaired Unicode in metadata without repairing resource identities", () => {
  for (const invalid of ["\ud800", "\udfff", "\ud800x", "\ud800\ud800"]) {
    expect(groupMetadataRows([{ table_schema: invalid, table_name: "users", columns: ["id"] }])).toEqual([]);
    expect(groupMetadataRows([{ table_schema: "public", table_name: invalid, columns: ["id"] }])).toEqual([]);
    for (const columns of [[invalid, "id"], JSON.stringify([invalid, "id"])]) {
      expect(groupMetadataRows([{ table_schema: "public", table_name: "users", columns }])).toEqual([
        { name: "public", tables: [{ name: "users", columns: ["id"] }] },
      ]);
    }
  }
  for (const valid of ["\ud83d\ude80", "\ufffd", "\\ud800"]) {
    expect(groupMetadataRows([{ table_schema: valid, table_name: valid, columns: JSON.stringify([valid]) }])).toEqual([
      { name: valid, tables: [{ name: valid, columns: [valid] }] },
    ]);
  }
});
