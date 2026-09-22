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
    const schema = metadataIdentityValue(row.table_schema ?? row.schema ?? row.database);
    const table = metadataIdentityValue(row.table_name ?? row.table);
    const type = cleanCompletionValue(row.table_type || row.type);
    if (!schema || !table) continue;
    const columns = metadataColumns(row);
    if (columns.length === 0) {
      suggestions.push({
        schema,
        table,
        column: metadataIdentityValue(row.column_name ?? row.column),
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
    const nameParts = splitSQLQualifiedName(match[1]).map(parseSQLIdentifier);
    const alias = parseSQLIdentifier(match[2] || "");
    const reference = {
      schema: nameParts.length > 1 ? nameParts[0].value : "",
      table: nameParts.length > 1 ? nameParts[1].value : nameParts[0]?.value || "",
      alias: isSQLAlias(alias.value) ? alias.value : "",
      schemaQuoted: nameParts.length > 1 ? nameParts[0].quoted : false,
      tableQuoted: nameParts.length > 1 ? nameParts[1].quoted : nameParts[0]?.quoted || false,
      aliasQuoted: isSQLAlias(alias.value) ? alias.quoted : false,
    };
    if (reference.table) references.push(reference);
  }
  return references;
}

export function pendingMetadataReferences(sql, rows, requestedKeys, limit = 4, identifierPolicy = "lowercase-unquoted") {
  return referencedTablesFromSQL(sql)
    .filter((reference) => reference.table && !metadataHasColumns(rows, reference, identifierPolicy))
    .filter((reference) => !requestedKeys.has(tableReferenceKey(reference, identifierPolicy)))
    .slice(0, limit);
}

export function normalizeSQLName(value) {
  return String(value || "")
    .trim()
    .toLowerCase();
}

export function tableReferenceKey(reference, identifierPolicy = "lowercase-unquoted") {
  return JSON.stringify([
    reference.schemaQuoted || identifierPolicy === "exact" ? "exact" : "folded",
    canonicalSQLIdentifier(reference.schema, reference.schemaQuoted, identifierPolicy),
    reference.tableQuoted || identifierPolicy === "exact" ? "exact" : "folded",
    canonicalSQLIdentifier(reference.table, reference.tableQuoted, identifierPolicy),
  ]);
}

export function tableMatchesReference(item, reference, identifierPolicy = "lowercase-unquoted") {
  if (!item || !reference) return false;
  const tableMatches = sqlIdentifierMatches(item.table, reference.table, reference.tableQuoted, identifierPolicy);
  if (!tableMatches) return false;
  if (reference.schema && !sqlIdentifierMatches(item.schema, reference.schema, reference.schemaQuoted, identifierPolicy)) return false;
  return true;
}

export function sqlIdentifierMatches(candidate, reference, quoted = false, identifierPolicy = "lowercase-unquoted") {
  return canonicalSQLIdentifier(candidate, true, identifierPolicy) === canonicalSQLIdentifier(reference, quoted, identifierPolicy);
}

export function sqlReferenceIdentifiersMatch(left, leftQuoted, right, rightQuoted, identifierPolicy = "lowercase-unquoted") {
  return canonicalSQLIdentifier(left, leftQuoted, identifierPolicy) === canonicalSQLIdentifier(right, rightQuoted, identifierPolicy);
}

export function sqlMetadataIdentity(value) {
  return String(value ?? "");
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
        return { name: metadataIdentityValue(item), dataType: "", position: index + 1 };
      }
      if (Array.isArray(item)) {
        return {
          position: numericPosition(item[0] || index + 1),
          name: metadataIdentityValue(item[1]),
          dataType: cleanCompletionValue(item[2]),
        };
      }
      return {
        name: metadataIdentityValue(item?.name ?? item?.column_name ?? item?.column),
        dataType: cleanCompletionValue(item?.data_type || item?.dataType || item?.type),
        position: numericPosition(item?.position || item?.ordinal_position || index + 1),
      };
    })
    .filter((item) => item.name);
}

function metadataHasColumns(rows, reference, identifierPolicy) {
  return (rows || []).some((item) => item.column && tableMatchesReference(item, reference, identifierPolicy));
}

function stripSQLStringsAndComments(sql) {
  return String(sql || "")
    .replace(/'([^']|'')*'/g, " ")
    .replace(/--.*$/gm, " ")
    .replace(/\/\*[\s\S]*?\*\//g, " ");
}

function splitSQLQualifiedName(value) {
  const parts = [];
  let current = "";
  let quote = "";
  const text = String(value || "");
  for (let index = 0; index < text.length; index += 1) {
    const character = text[index];
    if (quote) {
      current += character;
      if (character !== quote) continue;
      if (text[index + 1] === quote) {
        current += text[index + 1];
        index += 1;
      } else {
        quote = "";
      }
      continue;
    }
    if (character === '"' || character === "`") {
      quote = character;
      current += character;
    } else if (character === ".") {
      if (current.trim()) parts.push(current.trim());
      current = "";
    } else {
      current += character;
    }
  }
  if (current.trim()) parts.push(current.trim());
  return parts;
}

function parseSQLIdentifier(value) {
  const trimmed = String(value || "").trim();
  const quoted = (trimmed.startsWith('"') && trimmed.endsWith('"')) || (trimmed.startsWith("`") && trimmed.endsWith("`"));
  return { value: cleanSQLIdentifier(trimmed), quoted };
}

function canonicalSQLIdentifier(value, quoted, identifierPolicy) {
  return quoted || identifierPolicy === "exact" ? String(value || "") : normalizeSQLName(value);
}

function isSQLAlias(value) {
  if (!value) return false;
  return !new Set([
    "where",
    "join",
    "left",
    "right",
    "inner",
    "outer",
    "full",
    "cross",
    "on",
    "group",
    "order",
    "limit",
    "offset",
    "union",
    "having",
  ]).has(normalizeSQLName(value));
}

function cleanCompletionValue(value) {
  return String(value || "").trim();
}

function metadataIdentityValue(value) {
  return String(value ?? "");
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
