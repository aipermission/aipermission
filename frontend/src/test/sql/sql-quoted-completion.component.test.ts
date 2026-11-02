import { expect, it } from "vitest";
import { referencedTablesFromSQL } from "../../connectors/templates/_shared/sql-console-data";
import { sqlCompletionItems } from "../../connectors/templates/_shared/sql-editor-completions";

const monaco = { languages: { CompletionItemKind: { Keyword: 1, Module: 2, Class: 3, Field: 4 } } };
const metadata = [
  { schema: "dbo", table: "Orders", column: "order_id" },
  { schema: "private", table: "Orders", column: "private_id" },
  { schema: "dbo", table: "it's--/*table*/", column: "a]column" },
];

it.each(["SELECT * FROM ONLY dbo.", "SELECT * FROM ONLY(dbo."])("preserves PostgreSQL ONLY table context in %s", (sql) => {
  expect(
    complete(sql)
      .filter((item) => item.kind === 3)
      .map((item) => [item.label, item.detail]),
  ).toEqual([
    ["Orders", "dbo"],
    ["it's--/*table*/", "dbo"],
  ]);
  expect(referencedTablesFromSQL(sql + "Orders)").map((row) => [row.schema, row.table])).toEqual([["dbo", "Orders"]]);
});

it.each(["SELECT * FROM dbo.Orders o, private.", "SELECT * FROM dbo.Orders o JOIN dbo.Orders p ON o.order_id=p.order_id, private."])(
  "offers schema-scoped tables in %s, not expression lists",
  (sql) => {
    expect(
      complete(sql)
        .filter((item) => item.kind === 3)
        .map((item) => [item.label, item.detail]),
    ).toEqual([["Orders", "private"]]);
    expect(complete("SELECT COALESCE(x, private.").filter((item) => item.kind === 3)).toEqual([]);
  },
);

it("does not consume a preceding multiline comment when replacing a quoted identifier", () => {
  const sql = '/*\n" */ SELECT * FROM [Or] WHERE id = 1';
  const line = sql.split("\n")[1];
  const column = line.indexOf("Or") + 3;
  const items = sqlCompletionItems(
    monaco,
    metadata,
    [],
    {
      getValue: () => sql,
      getLineContent: () => line,
      getWordUntilPosition: () => ({ startColumn: column - 2, endColumn: column }),
    },
    { lineNumber: 2, column },
    "exact",
  );
  const item = items.find((item) => item.label === "Orders");
  if (!item) throw new Error("missing completion");
  expect(line.slice(0, item.range.startColumn - 1) + item.insertText + line.slice(item.range.endColumn - 1)).toBe(
    '" */ SELECT * FROM [Orders] WHERE id = 1',
  );
});

function complete(sql: string, cursor = sql.length) {
  return sqlCompletionItems(
    monaco,
    metadata,
    ["select"],
    {
      getValue: () => sql,
      getLineContent: () => sql,
      getWordUntilPosition: () => ({ startColumn: cursor + 1, endColumn: cursor + 1 }),
    },
    { lineNumber: 1, column: cursor + 1 },
    "exact",
  );
}

it.each(["it's", "a--b", "a/*b*/c", "a]]b", "FROM phantom"])("does not reinterpret bracket identifier %s", (table) => {
  const escaped = table.replaceAll("]", "]]");
  expect(
    referencedTablesFromSQL(`SELECT 'FROM ignored' FROM dbo.[${escaped}] AS [where] WHERE x='literal' -- FROM hidden\r\n/* JOIN nope */`),
  ).toEqual([{ schema: "dbo", table, alias: "where", schemaQuoted: false, tableQuoted: true, aliasQuoted: true }]);
});

it("ignores phantom table keywords inside quoted identifiers and incomplete table references", () => {
  expect(complete(",private.").filter((item) => item.kind === 3)).toEqual([]);
  expect(referencedTablesFromSQL('SELECT "FROM phantom", `JOIN hidden` FROM dbo.[unfinished')).toEqual([]);
  expect(referencedTablesFromSQL("SELECT * FROM dbo.")).toEqual([]);
  expect(referencedTablesFromSQL("SELECT * FROM a /* outer /* inner */ JOIN hidden */ JOIN b ON 1=1").map((row) => row.table)).toEqual([
    "a",
    "b",
  ]);
});

it.each([
  ["SELECT * FROM [Or", "SELECT * FROM [Orders]"],
  ['SELECT * FROM "Or', 'SELECT * FROM "Orders"'],
  ["SELECT * FROM `Or", "SELECT * FROM `Orders`"],
  ["SELECT * FROM dbo.[Or", "SELECT * FROM dbo.[Orders]"],
  ["SELECT * FROM [dbo].[Or", "SELECT * FROM [dbo].[Orders]"],
])("replaces the complete partial identifier in %s", (sql, expected) => {
  const completion = complete(sql).find((item) => item.label === "Orders" && item.detail === "dbo");
  expect(completion).toBeDefined();
  if (!completion) throw new Error("missing completion");
  expect(sql.slice(0, completion.range.startColumn - 1) + completion.insertText + sql.slice(completion.range.endColumn - 1)).toBe(expected);
  expect(completion.filterText).toBe(completion.insertText);
  if (sql.includes("dbo")) expect(complete(sql).some((item) => item.label === "dbo.Orders" || item.detail === "private")).toBe(false);
});

it("replaces the auto-inserted closer and remaining suffix instead of duplicating them", () => {
  const sql = "SELECT * FROM [Order]";
  const completion = complete(sql, sql.indexOf("der]")).find((item) => item.label === "Orders");
  if (!completion) throw new Error("missing completion");
  expect(completion.range).toMatchObject({ startColumn: 15, endColumn: sql.length + 1 });
  expect(sql.slice(0, completion.range.startColumn - 1) + completion.insertText + sql.slice(completion.range.endColumn - 1)).toBe(
    "SELECT * FROM [Orders]",
  );
});

it.each(['"a""lias"', "`a``lias`", "[a]]lias]"])("matches escaped qualifier %s only to its actual columns", (alias) => {
  const sql = `SELECT * FROM dbo.[it's--/*table*/] AS ${alias} WHERE ${alias}.[a`;
  const columns = complete(sql).filter((item) => item.kind === 4);
  expect(columns.map((item) => item.label)).toEqual(["a]column"]);
  expect(columns[0].insertText).toBe("[a]]column]");
});
