import assert from "node:assert/strict";
import test from "node:test";

import {
  extractTableSuggestions,
  pendingMetadataReferences,
  referencedTablesFromSQL,
  tableReferenceKey,
} from "../connectors/templates/_shared/sql-console-data.js";

test("SQL references support ANSI and ClickHouse quoted identifiers", () => {
  assert.deepEqual(referencedTablesFromSQL('SELECT * FROM "public"."users" AS u'), [
    { schema: "public", table: "users", alias: "u" },
  ]);
  assert.deepEqual(referencedTablesFromSQL("SELECT * FROM `analytics`.`daily-events` e"), [
    { schema: "analytics", table: "daily-events", alias: "e" },
  ]);
});

test("metadata requests are not reserved until the caller dispatches them", () => {
  const requested = new Set();
  const first = pendingMetadataReferences("SELECT * FROM analytics.events", [], requested);
  assert.equal(first.length, 1);
  assert.equal(requested.size, 0);

  requested.add(tableReferenceKey(first[0]));
  assert.deepEqual(pendingMetadataReferences("SELECT * FROM analytics.events", [], requested), []);
});

test("ClickHouse aggregated tuple metadata becomes ordered column suggestions", () => {
  const rows = extractTableSuggestions({
    rows: [{ database: "analytics", table_name: "events", columns: JSON.stringify([[2, "name", "String"], [1, "id", "UInt64"]]) }],
  });
  assert.deepEqual(rows, [
    { schema: "analytics", table: "events", column: "name", dataType: "String", position: 2, type: "" },
    { schema: "analytics", table: "events", column: "id", dataType: "UInt64", position: 1, type: "" },
  ]);
});
