type SQLRecord = Record<string, unknown>;
export type SQLIdentifierPolicy = "exact" | "lowercase-unquoted";
export type SQLTableReference = { schema: string; table: string };
export type SQLReference = SQLTableReference & {
  alias: string;
  schemaQuoted: boolean;
  tableQuoted: boolean;
  aliasQuoted: boolean;
};
export type SQLMetadataRow = SQLTableReference & {
  column: string;
  dataType: string;
  position: number;
  type: string;
};
type SQLColumn = { name: string; dataType: string; position: number };
type SQLReferenceInput = SQLTableReference & { schemaQuoted?: boolean; tableQuoted?: boolean };

function isRecord(value: unknown): value is SQLRecord {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

export function normalizeConnectorOutput(output: unknown): SQLRecord {
  if (typeof output !== "string") return isRecord(output) ? output : {};
  try {
    const parsed: unknown = JSON.parse(output);
    return isRecord(parsed) ? parsed : {};
  } catch {
    return {};
  }
}

export function extractTableSuggestions(output: unknown): SQLMetadataRow[] {
  const normalized = normalizeConnectorOutput(output);
  const rows = Array.isArray(normalized?.rows) ? normalized.rows : [];
  const suggestions: SQLMetadataRow[] = [];
  for (const item of rows) {
    if (!isRecord(item)) continue;
    const row = item;
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

export function referencedTablesFromSQL(sql: string): SQLReference[] {
  const cleaned = stripSQLStringsAndComments(sql);
  const identifier = '(?:"(?:[^"]|"")+"|`(?:[^`]|``)+`|[a-zA-Z_][\\w$]*)';
  const pattern = new RegExp(`\\b(?:from|join)\\s+(${identifier}(?:\\s*\\.\\s*${identifier})?)(?:\\s+(?:as\\s+)?(${identifier}))?`, "gi");
  const references: SQLReference[] = [];
  for (const match of cleaned.matchAll(pattern)) {
    const nameParts = splitSQLQualifiedName(match[1] || "").map(parseSQLIdentifier);
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

export function pendingMetadataReferences(
  sql: string,
  rows: SQLMetadataRow[],
  requestedKeys: Set<string>,
  limit = 4,
  identifierPolicy: SQLIdentifierPolicy = "lowercase-unquoted",
): SQLReference[] {
  return referencedTablesFromSQL(sql)
    .filter((reference) => reference.table && !metadataHasColumns(rows, reference, identifierPolicy))
    .filter((reference) => !requestedKeys.has(tableReferenceKey(reference, identifierPolicy)))
    .slice(0, limit);
}

export function normalizeSQLName(value: unknown): string {
  return String(value || "")
    .trim()
    .toLowerCase();
}

export function tableReferenceKey(reference: SQLReferenceInput, identifierPolicy: SQLIdentifierPolicy = "lowercase-unquoted"): string {
  return JSON.stringify([
    reference.schemaQuoted || identifierPolicy === "exact" ? "exact" : "folded",
    canonicalSQLIdentifier(reference.schema, Boolean(reference.schemaQuoted), identifierPolicy),
    reference.tableQuoted || identifierPolicy === "exact" ? "exact" : "folded",
    canonicalSQLIdentifier(reference.table, Boolean(reference.tableQuoted), identifierPolicy),
  ]);
}

export function tableMatchesReference(
  item: SQLTableReference | null | undefined,
  reference: SQLReferenceInput | null | undefined,
  identifierPolicy: SQLIdentifierPolicy = "lowercase-unquoted",
): boolean {
  if (!item || !reference) return false;
  const tableMatches = sqlIdentifierMatches(item.table, reference.table, Boolean(reference.tableQuoted), identifierPolicy);
  if (!tableMatches) return false;
  if (reference.schema && !sqlIdentifierMatches(item.schema, reference.schema, Boolean(reference.schemaQuoted), identifierPolicy)) return false;
  return true;
}

export function sqlIdentifierMatches(candidate: string, reference: string, quoted = false, identifierPolicy: SQLIdentifierPolicy = "lowercase-unquoted"): boolean {
  return canonicalSQLIdentifier(candidate, true, identifierPolicy) === canonicalSQLIdentifier(reference, quoted, identifierPolicy);
}

export function sqlReferenceIdentifiersMatch(left: string, leftQuoted: boolean, right: string, rightQuoted: boolean, identifierPolicy: SQLIdentifierPolicy = "lowercase-unquoted"): boolean {
  return canonicalSQLIdentifier(left, leftQuoted, identifierPolicy) === canonicalSQLIdentifier(right, rightQuoted, identifierPolicy);
}

export function sqlMetadataIdentity(value: unknown): string {
  return String(value ?? "");
}

export function cleanSQLIdentifier(value: unknown): string {
  const trimmed = String(value || "").trim();
  if (trimmed.startsWith('"') && trimmed.endsWith('"')) {
    return trimmed.slice(1, -1).replaceAll('""', '"');
  }
  if (trimmed.startsWith("`") && trimmed.endsWith("`")) {
    return trimmed.slice(1, -1).replaceAll("``", "`");
  }
  return trimmed;
}

function metadataColumns(row: SQLRecord): SQLColumn[] {
  const columns = row.columns;
  if (!columns) return [];
  const parsed = typeof columns === "string" ? parseJSON(columns) : columns;
  if (!Array.isArray(parsed)) return [];
  return parsed
    .map((item: unknown, index: number): SQLColumn => {
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
      return isRecord(item)
        ? {
            name: metadataIdentityValue(item.name ?? item.column_name ?? item.column),
            dataType: cleanCompletionValue(item.data_type || item.dataType || item.type),
            position: numericPosition(item.position || item.ordinal_position || index + 1),
          }
        : { name: "", dataType: "", position: 0 };
    })
    .filter((item) => item.name);
}

function metadataHasColumns(rows: SQLMetadataRow[], reference: SQLReferenceInput, identifierPolicy: SQLIdentifierPolicy): boolean {
  return (rows || []).some((item) => item.column && tableMatchesReference(item, reference, identifierPolicy));
}

function stripSQLStringsAndComments(sql: string): string {
  return String(sql || "")
    .replace(/'([^']|'')*'/g, " ")
    .replace(/--.*$/gm, " ")
    .replace(/\/\*[\s\S]*?\*\//g, " ");
}

function splitSQLQualifiedName(value: string): string[] {
  const parts: string[] = [];
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

function parseSQLIdentifier(value: string): { value: string; quoted: boolean } {
  const trimmed = String(value || "").trim();
  const quoted = (trimmed.startsWith('"') && trimmed.endsWith('"')) || (trimmed.startsWith("`") && trimmed.endsWith("`"));
  return { value: cleanSQLIdentifier(trimmed), quoted };
}

function canonicalSQLIdentifier(value: string, quoted: boolean, identifierPolicy: SQLIdentifierPolicy): string {
  return quoted || identifierPolicy === "exact" ? String(value || "") : normalizeSQLName(value);
}

function isSQLAlias(value: string): boolean {
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

function cleanCompletionValue(value: unknown): string {
  return String(value || "").trim();
}

function metadataIdentityValue(value: unknown): string {
  return String(value ?? "");
}

function numericPosition(value: unknown): number {
  const number = Number(value);
  return Number.isFinite(number) && number > 0 ? number : 0;
}

function parseJSON(value: string): unknown {
  try {
    return JSON.parse(value);
  } catch {
    return null;
  }
}
