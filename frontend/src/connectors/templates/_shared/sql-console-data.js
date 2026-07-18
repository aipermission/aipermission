export function normalizeConnectorOutput(output) {
  if (typeof output !== "string") return output || {};
  try {
    return JSON.parse(output);
  } catch {
    return {};
  }
}

export function extractTableSuggestions(output) {
  const normalized = normalizeConnectorOutput(output);
  const rows = Array.isArray(normalized?.rows) ? normalized.rows : [];
  const suggestions = [];
  for (const row of rows) {
    const schema = cleanCompletionValue(row.table_schema || row.schema || row.database);
    const table = cleanCompletionValue(row.table_name || row.table);
    const type = cleanCompletionValue(row.table_type || row.type);
    if (!schema || !table) continue;
    const columns = metadataColumns(row);
    if (columns.length === 0) {
      suggestions.push({
        schema,
        table,
        column: cleanCompletionValue(row.column_name || row.column),
        dataType: cleanCompletionValue(row.data_type || ""),
        position: numericPosition(row.ordinal_position || row.position),
        type,
      });
      continue;
    }
    for (const column of columns) {
      suggestions.push({
        schema,
        table,
        column: column.name,
        dataType: column.dataType,
        position: column.position,
        type,
      });
    }
  }
  return suggestions.filter((row) => row.schema && row.table);
}

export function referencedTablesFromSQL(sql) {
  const cleaned = stripSQLStringsAndComments(sql);
  const identifier = '(?:"(?:[^"]|"")+"|`(?:[^`]|``)+`|[a-zA-Z_][\\w$]*)';
  const pattern = new RegExp(`\\b(?:from|join)\\s+(${identifier}(?:\\s*\\.\\s*${identifier})?)(?:\\s+(?:as\\s+)?(${identifier}))?`, "gi");
  const references = [];
  for (const match of cleaned.matchAll(pattern)) {
    const nameParts = splitSQLQualifiedName(match[1]);
    const alias = cleanSQLIdentifier(match[2] || "");
    const reference = {
      schema: nameParts.length > 1 ? nameParts[0] : "",
      table: nameParts.length > 1 ? nameParts[1] : nameParts[0],
      alias: isSQLAlias(alias) ? alias : "",
    };
    if (reference.table) references.push(reference);
  }
  return references;
}

export function pendingMetadataReferences(sql, rows, requestedKeys, limit = 4) {
  return referencedTablesFromSQL(sql)
    .filter((reference) => reference.table && !metadataHasColumns(rows, reference))
    .filter((reference) => !requestedKeys.has(tableReferenceKey(reference)))
    .slice(0, limit);
}

export function normalizeSQLName(value) {
  return String(value || "").trim().toLowerCase();
}

export function tableReferenceKey(reference) {
  return `${normalizeSQLName(reference.schema)}.${normalizeSQLName(reference.table)}`;
}

export function tableMatchesReference(item, reference) {
  if (!item || !reference) return false;
  const tableMatches = normalizeSQLName(item.table) === normalizeSQLName(reference.table);
  if (!tableMatches) return false;
  if (reference.schema && normalizeSQLName(item.schema) !== normalizeSQLName(reference.schema)) return false;
  return true;
}

export function cleanSQLIdentifier(value) {
  const trimmed = String(value || "").trim();
  if (trimmed.startsWith('"') && trimmed.endsWith('"')) {
    return trimmed.slice(1, -1).replaceAll('""', '"');
  }
  if (trimmed.startsWith("`") && trimmed.endsWith("`")) {
    return trimmed.slice(1, -1).replaceAll("``", "`");
  }
  return trimmed;
}

function metadataColumns(row) {
  const columns = row.columns;
  if (!columns) return [];
  const parsed = typeof columns === "string" ? parseJSON(columns) : columns;
  if (!Array.isArray(parsed)) return [];
  return parsed
    .map((item, index) => {
      if (typeof item === "string") {
        return { name: cleanCompletionValue(item), dataType: "", position: index + 1 };
      }
      if (Array.isArray(item)) {
        return {
          position: numericPosition(item[0] || index + 1),
          name: cleanCompletionValue(item[1]),
          dataType: cleanCompletionValue(item[2]),
        };
      }
      return {
        name: cleanCompletionValue(item?.name || item?.column_name || item?.column),
        dataType: cleanCompletionValue(item?.data_type || item?.dataType || item?.type),
        position: numericPosition(item?.position || item?.ordinal_position || index + 1),
      };
    })
    .filter((item) => item.name);
}

function metadataHasColumns(rows, reference) {
  return (rows || []).some((item) => item.column && tableMatchesReference(item, reference));
}

function stripSQLStringsAndComments(sql) {
  return String(sql || "")
    .replace(/'([^']|'')*'/g, " ")
    .replace(/--.*$/gm, " ")
    .replace(/\/\*[\s\S]*?\*\//g, " ");
}

function splitSQLQualifiedName(value) {
  return String(value || "")
    .split(".")
    .map((part) => cleanSQLIdentifier(part))
    .filter(Boolean);
}

function isSQLAlias(value) {
  if (!value) return false;
  return !new Set(["where", "join", "left", "right", "inner", "outer", "full", "cross", "on", "group", "order", "limit", "offset", "union", "having"]).has(normalizeSQLName(value));
}

function cleanCompletionValue(value) {
  return String(value || "").trim();
}

function numericPosition(value) {
  const number = Number(value);
  return Number.isFinite(number) && number > 0 ? number : 0;
}

function parseJSON(value) {
  try {
    return JSON.parse(value);
  } catch {
    return null;
  }
}
