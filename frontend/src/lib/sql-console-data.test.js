import assert from "node:assert/strict";
import test from "node:test";

import {
  extractTableSuggestions,
  pendingMetadataReferences,
  referencedTablesFromSQL,
  tableReferenceKey,
} from "../connectors/templates/_shared/sql-console-data.js";
import { filteredTableBrowserRows, normalizeSQLConsoleConfig } from "../connectors/templates/_shared/sql-console-config.js";
import { sqlCompletionItems } from "../connectors/templates/_shared/sql-editor-completions.js";

test("SQL references support ANSI and ClickHouse quoted identifiers", () => {
  assert.deepEqual(referencedTablesFromSQL('SELECT * FROM "public"."users" AS u'), [{ schema: "public", table: "users", alias: "u" }]);
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
    rows: [
      {
        database: "analytics",
        table_name: "events",
        columns: JSON.stringify([
          [2, "name", "String"],
          [1, "id", "UInt64"],
        ]),
      },
    ],
  });
  assert.deepEqual(rows, [
    { schema: "analytics", table: "events", column: "name", dataType: "String", position: 2, type: "" },
    { schema: "analytics", table: "events", column: "id", dataType: "UInt64", position: 1, type: "" },
  ]);
});

test("SQL metadata preserves whitespace-bearing database identities end to end", () => {
  const rows = extractTableSuggestions({
    rows: [{ table_schema: " tenant ", table_name: " order lines ", column_name: " item id ", data_type: " text " }],
  });
  assert.deepEqual(rows, [{ schema: " tenant ", table: " order lines ", column: " item id ", dataType: "text", position: 0, type: "" }]);

  const browserRows = filteredTableBrowserRows(rows, "order");
  assert.equal(browserRows[0].schema, " tenant ");
  assert.equal(browserRows[0].table, " order lines ");
  assert.equal(browserRows[0].columns[0].name, " item id ");
  assert.equal(normalizeSQLConsoleConfig().tableQuery(browserRows[0], 25), 'SELECT *\nFROM " tenant "." order lines "\nLIMIT 25;');

  const monaco = { languages: { CompletionItemKind: { Keyword: 1, Module: 2, Class: 3, Field: 4 } } };
  const model = {
    getValue: () => "SELECT * FROM ",
    getWordUntilPosition: () => ({ startColumn: 15, endColumn: 15 }),
    getLineContent: () => "SELECT * FROM ",
  };
  const suggestions = sqlCompletionItems(monaco, rows, [], model, { lineNumber: 1, column: 15 });
  assert.equal(suggestions.find((item) => item.label === " order lines ")?.insertText, '" order lines "');
  assert.equal(suggestions.find((item) => item.label === " tenant . order lines ")?.insertText, '" tenant "." order lines "');
});
