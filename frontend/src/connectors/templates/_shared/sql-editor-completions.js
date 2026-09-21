import {
  cleanSQLIdentifier,
  referencedTablesFromSQL,
  sqlIdentifierMatches,
  sqlReferenceIdentifiersMatch,
  tableMatchesReference,
  tableReferenceKey,
} from "./sql-console-data.js";

export function sqlCompletionItems(monaco, tables, keywords, model, position, identifierPolicy = "lowercase-unquoted") {
  const word = model.getWordUntilPosition(position);
  const range = {
    startLineNumber: position.lineNumber,
    endLineNumber: position.lineNumber,
    startColumn: word.startColumn,
    endColumn: word.endColumn,
  };
  const suggestions = keywords.map((keyword) => ({
    label: keyword.toUpperCase(),
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: keyword,
    sortText: `2_${keyword}`,
    range,
  }));
  const seenSchemas = new Set();
  const seenTables = new Set();
  const seenColumns = new Set();
  const tableReferences = referencedTablesFromSQL(model.getValue());
  const dotReference = dotReferenceBeforePosition(model, position);
  const columnReferences = dotReference
    ? matchingReferencesForQualifier(dotReference, tableReferences, tables, identifierPolicy)
    : tableReferences;
  const inTableContext = isTableCompletionContext(model, position);
  for (const item of tables || []) {
    addSchemaSuggestion(suggestions, seenSchemas, item, monaco, range);
    addTableSuggestions(suggestions, seenTables, item, monaco, range);
    if (!inTableContext && item.column && columnReferences.some((reference) => tableMatchesReference(item, reference, identifierPolicy))) {
      addColumnSuggestion(suggestions, seenColumns, item, monaco, range);
    }
  }
  return suggestions;
}

function addSchemaSuggestion(suggestions, seen, item, monaco, range) {
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

function addTableSuggestions(suggestions, seen, item, monaco, range) {
  const tableKey = `${item.schema}.${item.table}`;
  const identity = JSON.stringify([item.schema, item.table]);
  if (seen.has(identity)) return;
  seen.add(identity);
  suggestions.push(
    {
      label: item.table,
      kind: monaco.languages.CompletionItemKind.Class,
      insertText: quoteSQLIdentifier(item.table),
      detail: item.schema,
      documentation: item.type || "table",
      sortText: `0_table_${item.table}`,
      range,
    },
    {
      label: tableKey,
      kind: monaco.languages.CompletionItemKind.Class,
      insertText: `${quoteSQLIdentifier(item.schema)}.${quoteSQLIdentifier(item.table)}`,
      detail: item.type || "table",
      sortText: `0_full_${tableKey}`,
      range,
    },
  );
}

function addColumnSuggestion(suggestions, seen, item, monaco, range) {
  const tableKey = `${item.schema}.${item.table}`;
  const columnKey = JSON.stringify([item.schema, item.table, item.column]);
  if (seen.has(columnKey)) return;
  seen.add(columnKey);
  suggestions.push({
    label: item.column,
    kind: monaco.languages.CompletionItemKind.Field,
    insertText: quoteSQLIdentifier(item.column),
    detail: `${tableKey}${item.dataType ? ` / ${item.dataType}` : ""}`,
    sortText: `0_column_${item.column}_${columnKey}`,
    range,
  });
}

function matchingReferencesForQualifier(qualifier, references, metadataRows, identifierPolicy) {
  const matches = references.filter(
    (reference) =>
      sqlReferenceIdentifiersMatch(reference.alias, reference.aliasQuoted, qualifier.value, qualifier.quoted, identifierPolicy) ||
      sqlReferenceIdentifiersMatch(reference.table, reference.tableQuoted, qualifier.value, qualifier.quoted, identifierPolicy),
  );
  if (matches.length > 0) return matches;
  const metadataMatches = [];
  const seen = new Set();
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

function dotReferenceBeforePosition(model, position) {
  const prefix = model.getLineContent(position.lineNumber).slice(0, position.column - 1);
  const match = prefix.match(/((?:"[^"]+"|`[^`]+`|[a-zA-Z_][\w$]*))\.\s*(?:"[^"]*"|`[^`]*`|[a-zA-Z_][\w$]*)?$/);
  if (!match) return null;
  const raw = match[1];
  return {
    value: cleanSQLIdentifier(raw),
    quoted: (raw.startsWith('"') && raw.endsWith('"')) || (raw.startsWith("`") && raw.endsWith("`")),
  };
}

function isTableCompletionContext(model, position) {
  const prefix = model
    .getLineContent(position.lineNumber)
    .slice(0, position.column - 1)
    .toLowerCase();
  return /\b(from|join)\s+(?:"[^"]*"|`[^`]*`|[a-z_][\w$]*)?$/i.test(prefix);
}

function quoteSQLIdentifier(value) {
  return /^[a-z_][a-z0-9_]*$/.test(value) ? value : `"${String(value).replaceAll('"', '""')}"`;
}
