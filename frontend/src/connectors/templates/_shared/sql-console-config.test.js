import assert from "node:assert/strict";
import test from "node:test";
import { filteredTableBrowserRows, mergeMetadataRows } from "./sql-console-config.js";

test("SQL browser identity preserves case-distinct and dotted identifiers", () => {
  const rows = filteredTableBrowserRows(
    [
      { schema: "public", table: "Users", column: "Admin.Secret", position: 1 },
      { schema: "public", table: "users", column: "public_name", position: 1 },
      { schema: "tenant.one", table: "events.live", column: "id", position: 1 },
    ],
    "",
  );

  assert.deepEqual(
    rows.map(({ schema, table, columns }) => ({ schema, table, columns: columns.map((column) => column.name) })),
    [
      { schema: "public", table: "users", columns: ["public_name"] },
      { schema: "public", table: "Users", columns: ["Admin.Secret"] },
      { schema: "tenant.one", table: "events.live", columns: ["id"] },
    ],
  );
});

test("metadata merging keeps exact case-distinct identities", () => {
  assert.equal(
    mergeMetadataRows([{ schema: "public", table: "Users", column: "ID" }], [{ schema: "public", table: "users", column: "id" }]).length,
    2,
  );
});
