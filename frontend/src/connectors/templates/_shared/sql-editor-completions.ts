import {
  cleanSQLIdentifier,
  referencedTablesFromSQL,
  sqlIdentifierMatches,
  sqlReferenceIdentifiersMatch,
  tableMatchesReference,
  tableReferenceKey,
} from "./sql-console-data.ts";
import type { SQLIdentifierPolicy, SQLReference, SQLTableReference } from "./sql-console-data.ts";
import { sqlContextTokens, sqlKeyword } from "./sql-lexical-context.ts";

type CompletionPosition = { lineNumber: number; column: number };
type CompletionRange = { startLineNumber: number; endLineNumber: number; startColumn: number; endColumn: number };
type CompletionItem = {
  label: string;
  kind: number;
  insertText: string;
  sortText: string;
  range: CompletionRange;
  detail?: string;
  documentation?: string;
  filterText?: string;
};
type CompletionMonaco = {
  languages: { CompletionItemKind: { Keyword: number; Module: number; Class: number; Field: number } };
};
type CompletionModel = {
  getValue(): string;
  getLineContent(_lineNumber: number): string;
  getWordUntilPosition(_position: CompletionPosition): { startColumn: number; endColumn: number };
};
type CompletionRow = SQLTableReference & { column?: string; dataType?: string; type?: string };
type Qualifier = { value: string; quoted: boolean };

export function sqlCompletionItems(
  monaco: CompletionMonaco,
  tables: readonly CompletionRow[],
  keywords: readonly string[],
  model: CompletionModel,
  position: CompletionPosition,
  identifierPolicy: SQLIdentifierPolicy = "lowercase-unquoted",
): CompletionItem[] {
  const context = completionContext(model, position);
  const { range, opener, qualifier: dotReference, inTableContext } = context;
  const suggestions: CompletionItem[] = (opener ? [] : keywords).map((keyword) => ({
    label: keyword.toUpperCase(),
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: keyword,
    sortText: `2_${keyword}`,
    range,
  }));
  const seenSchemas = new Set<string>();
  const seenTables = new Set<string>();
  const seenColumns = new Set<string>();
  const tableReferences = referencedTablesFromSQL(model.getValue());
  const columnReferences = dotReference
    ? matchingReferencesForQualifier(dotReference, tableReferences, tables, identifierPolicy)
    : tableReferences;
  for (const item of tables || []) {
    if (!dotReference) addSchemaSuggestion(suggestions, seenSchemas, item, monaco, range);
    if (!dotReference || (inTableContext && sqlIdentifierMatches(item.schema, dotReference.value, dotReference.quoted, identifierPolicy))) {
      addTableSuggestions(suggestions, seenTables, item, monaco, range, !dotReference && !opener);
    }
    if (!inTableContext && item.column && columnReferences.some((reference) => tableMatchesReference(item, reference, identifierPolicy))) {
      addColumnSuggestion(suggestions, seenColumns, item, monaco, range);
    }
  }
  return suggestions.map((item) => {
    if (!opener || item.kind === monaco.languages.CompletionItemKind.Keyword) return item;
    const name = cleanSQLIdentifier(item.insertText);
    const closer = opener === "[" ? "]" : opener;
    const insertText = `${opener}${name.replaceAll(closer, closer + closer)}${closer}`;
    return { ...item, insertText, filterText: insertText };
  });
}

function addSchemaSuggestion(
  suggestions: CompletionItem[],
  seen: Set<string>,
  item: CompletionRow,
  monaco: CompletionMonaco,
  range: CompletionRange,
): void {
  if (!item.schema || seen.has(item.schema)) return;
  seen.add(item.schema);
  suggestions.push({
    label: item.schema,
    kind: monaco.languages.CompletionItemKind.Module,
    insertText: quoteSQLIdentifier(item.schema),
    detail: "schema",
    sortText: `1_schema_${item.schema}`,
    range,
  });
}

function addTableSuggestions(
  suggestions: CompletionItem[],
  seen: Set<string>,
  item: CompletionRow,
  monaco: CompletionMonaco,
  range: CompletionRange,
  includeQualified: boolean,
): void {
  const tableKey = `${item.schema}.${item.table}`;
  const identity = JSON.stringify([item.schema, item.table]);
  if (seen.has(identity)) return;
  seen.add(identity);
  suggestions.push({
    label: item.table,
    kind: monaco.languages.CompletionItemKind.Class,
    insertText: quoteSQLIdentifier(item.table),
    detail: item.schema,
    documentation: item.type || "table",
    sortText: `0_table_${item.table}`,
    range,
  });
  if (includeQualified)
    suggestions.push({
      label: tableKey,
      kind: monaco.languages.CompletionItemKind.Class,
      insertText: `${quoteSQLIdentifier(item.schema)}.${quoteSQLIdentifier(item.table)}`,
      detail: item.type || "table",
      sortText: `0_full_${tableKey}`,
      range,
    });
}

function addColumnSuggestion(
  suggestions: CompletionItem[],
  seen: Set<string>,
  item: CompletionRow,
  monaco: CompletionMonaco,
  range: CompletionRange,
): void {
  const column = item.column;
  if (!column) return;
  const tableKey = `${item.schema}.${item.table}`;
  const columnKey = JSON.stringify([item.schema, item.table, column]);
  if (seen.has(columnKey)) return;
  seen.add(columnKey);
  suggestions.push({
    label: column,
    kind: monaco.languages.CompletionItemKind.Field,
    insertText: quoteSQLIdentifier(column),
    detail: `${tableKey}${item.dataType ? ` / ${item.dataType}` : ""}`,
    sortText: `0_column_${column}_${columnKey}`,
    range,
  });
}

function matchingReferencesForQualifier(
  qualifier: Qualifier,
  references: SQLReference[],
  metadataRows: readonly CompletionRow[],
  identifierPolicy: SQLIdentifierPolicy,
): SQLReference[] {
  const matches = references.filter(
    (reference) =>
      sqlReferenceIdentifiersMatch(reference.alias, reference.aliasQuoted, qualifier.value, qualifier.quoted, identifierPolicy) ||
      sqlReferenceIdentifiersMatch(reference.table, reference.tableQuoted, qualifier.value, qualifier.quoted, identifierPolicy),
  );
  if (matches.length > 0) return matches;
  const metadataMatches: SQLReference[] = [];
  const seen = new Set<string>();
  for (const item of metadataRows || []) {
    if (!sqlIdentifierMatches(item.table, qualifier.value, qualifier.quoted, identifierPolicy)) continue;
    const reference = {
      schema: item.schema || "",
      table: item.table || "",
      alias: "",
      schemaQuoted: true,
      tableQuoted: true,
      aliasQuoted: false,
    };
    const key = tableReferenceKey(reference, identifierPolicy);
    if (seen.has(key)) continue;
    seen.add(key);
    metadataMatches.push(reference);
  }
  return metadataMatches;
}

function completionContext(model: CompletionModel, position: CompletionPosition) {
  const line = model.getLineContent(position.lineNumber);
  const cursor = Math.min(line.length, position.column - 1);
  const document = model.getValue();
  const previousLines = document.split("\n").slice(0, position.lineNumber - 1);
  const lineOffset = previousLines.length ? previousLines.join("\n").length + 1 : 0;
  const documentTokens = sqlContextTokens(document);
  const active = documentTokens.find(
    (token) =>
      token.kind === "identifier" &&
      token.start >= lineOffset &&
      token.end <= lineOffset + line.length &&
      token.start < lineOffset + cursor &&
      lineOffset + cursor <= token.end,
  );
  const word = model.getWordUntilPosition(position);
  const range = {
    startLineNumber: position.lineNumber,
    endLineNumber: position.lineNumber,
    startColumn: active ? active.start - lineOffset + 1 : word.startColumn,
    endColumn: active ? active.end - lineOffset + 1 : word.endColumn,
  };
  const tokens = documentTokens.filter((token) => token.end <= lineOffset + range.startColumn - 1);
  const last = tokens.at(-1);
  const previous = tokens.at(-2);
  const qualifier: Qualifier | null =
    last?.text === "." && previous?.kind === "identifier" && previous.complete
      ? { value: cleanSQLIdentifier(previous.text), quoted: previous.quoted }
      : null;
  let tableIndex = tokens.length - (qualifier ? 3 : 1);
  if (tokens[tableIndex]?.text === "(" && sqlKeyword(tokens[tableIndex - 1], "only")) tableIndex--;
  if (sqlKeyword(tokens[tableIndex], "only")) tableIndex--;
  const tableToken = tokens[tableIndex];
  return {
    range,
    opener: active?.quoted ? active.text[0] : "",
    qualifier,
    inTableContext:
      sqlKeyword(tableToken, "from") ||
      sqlKeyword(tableToken, "join") ||
      (tableToken?.text === "," && inCommaTableList(tokens.slice(0, qualifier ? -3 : -1))),
  };
}

function inCommaTableList(tokens: ReturnType<typeof sqlContextTokens>): boolean {
  let depth = 0;
  for (let index = tokens.length - 1; index >= 0; index--) {
    const token = tokens[index];
    if (token.text === ")") depth++;
    else if (token.text === "(") {
      if (!depth) return false;
      depth--;
    } else if (!depth && !token.quoted && token.kind === "identifier") {
      if (["from", "join"].includes(token.text.toLowerCase())) return true;
      if (["where", "select", "group", "order", "having", "union"].includes(token.text.toLowerCase())) return false;
    }
  }
  return false;
}

function quoteSQLIdentifier(value: string): string {
  return /^[a-z_][a-z0-9_]*$/.test(value) ? value : `"${String(value).replaceAll('"', '""')}"`;
}
