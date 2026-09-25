import { expect, it } from "vitest";
import {
  cleanSQLIdentifier,
  extractTableSuggestions,
  normalizeConnectorOutput,
  pendingMetadataReferences,
  referencedTablesFromSQL,
  sqlMetadataIdentity,
  sqlReferenceIdentifiersMatch,
  tableMatchesReference,
  tableReferenceKey,
} from "./sql-console-data";

it("extracts ordered column identities from mixed connector metadata formats", () => {
  expect(
    extractTableSuggestions({
      rows: [
        {
          table_schema: "public",
          table_name: "users",
          table_type: "BASE TABLE",
          columns: JSON.stringify(["id", [2, "email", "text"], { column_name: "Name", dataType: "varchar", ordinal_position: 3 }, null]),
        },
        { database: "analytics", table: "events", column: "payload", data_type: "JSON", position: "invalid", columns: "invalid-json" },
        { schema: "public", table: "audit", column_name: "id", ordinal_position: 1, columns: {} },
        { table: "missing-schema" },
        null,
      ],
    }),
  ).toEqual([
    { schema: "public", table: "users", column: "id", dataType: "", position: 1, type: "BASE TABLE" },
    { schema: "public", table: "users", column: "email", dataType: "text", position: 2, type: "BASE TABLE" },
    { schema: "public", table: "users", column: "Name", dataType: "varchar", position: 3, type: "BASE TABLE" },
    { schema: "analytics", table: "events", column: "payload", dataType: "JSON", position: 0, type: "" },
    { schema: "public", table: "audit", column: "id", dataType: "", position: 1, type: "" },
  ]);
});

it.each([null, [], "null", "[]", "invalid-json"])("does not turn malformed output %j into suggestions", (output) => {
  expect(normalizeConnectorOutput(output)).toEqual({});
  expect(extractTableSuggestions(output)).toEqual([]);
});

it("preserves escaped qualified identifiers while ignoring references inside strings and comments", () => {
  const sql =
    'SELECT \'from secrets\' FROM "tenant.one"."Us""ers" AS "u" JOIN `archive`.`events``old` e ON true -- JOIN ignored\n/* FROM hidden */';
  expect(referencedTablesFromSQL(sql)).toEqual([
    { schema: "tenant.one", table: 'Us"ers', alias: "u", schemaQuoted: true, tableQuoted: true, aliasQuoted: true },
    { schema: "archive", table: "events`old", alias: "e", schemaQuoted: true, tableQuoted: true, aliasQuoted: false },
  ]);
  expect(cleanSQLIdentifier('"A""B"')).toBe('A"B');
  expect(cleanSQLIdentifier("`A``B`")).toBe("A`B");
  expect(sqlMetadataIdentity(0)).toBe("0");
});

it("bounds pending metadata requests and skips described or already requested tables", () => {
  const rows = [{ schema: "public", table: "users", column: "id", dataType: "integer", position: 1, type: "BASE TABLE" }];
  const requested = new Set([tableReferenceKey({ schema: "", table: "orders" })]);
  const pending = pendingMetadataReferences(
    "SELECT * FROM users JOIN orders ON true JOIN logs ON true JOIN archive ON true",
    rows,
    requested,
    1,
  );
  expect(pending).toEqual([{ schema: "", table: "logs", alias: "", schemaQuoted: false, tableQuoted: false, aliasQuoted: false }]);
  expect(requested).toEqual(new Set([tableReferenceKey({ schema: "", table: "orders" })]));
  expect(tableMatchesReference(null, { schema: "public", table: "users" })).toBe(false);
  expect(tableMatchesReference({ schema: "public", table: "users" }, null)).toBe(false);
  expect(tableMatchesReference({ schema: "public", table: "users" }, { schema: "private", table: "users" })).toBe(false);
  expect(sqlReferenceIdentifiersMatch("USERS", false, "users", false)).toBe(true);
  expect(sqlReferenceIdentifiersMatch("USERS", true, "users", false)).toBe(false);
  expect(sqlReferenceIdentifiersMatch("USERS", false, "users", false, "exact")).toBe(false);
});
