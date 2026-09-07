import { normalizeSQLName } from "./sql-console-data";

export function normalizeSQLConsoleConfig(config = {}) {
  const label = String(config.label || "SQL").trim() || "SQL";
  const defaultPort = Number(config.defaultPort) || 0;
  const defaultDatabase = String(config.defaultDatabase || "database");
  return {
    label,
    queryAction: String(config.queryAction || "query_readonly"),
    describeAction: String(config.describeAction || "describe_table"),
    metadataSQL: String(config.metadataSQL || ""),
    metadataMaxRows: Math.max(1, Number(config.metadataMaxRows) || 5000),
    metadataReason: String(config.metadataReason || `load ${label} console autocomplete`),
    manualReason: String(config.manualReason || `manual ${label} console query`),
    browserLabel: String(config.browserLabel || "Schema"),
    filenamePrefix: String(config.filenamePrefix || `${label.toLowerCase()}-result`),
    keywords: [...new Set([...DEFAULT_SQL_KEYWORDS, ...(config.keywords || [])].map((item) => String(item).toLowerCase()))],
    targetEndpoint: config.targetEndpoint || ((target) => defaultTargetEndpoint(target, defaultPort, defaultDatabase)),
    tableQuery:
      config.tableQuery ||
      ((table, maxRows) => `SELECT *\nFROM ${quoteSQLIdentifier(table.schema)}.${quoteSQLIdentifier(table.table)}\nLIMIT ${maxRows};`),
    describeInput: config.describeInput || ((reference) => ({ schema: reference.schema || "", table: reference.table })),
  };
}

export function metadataStatusText(metadata, config) {
  if (metadata.state === "loading") return "Loading metadata suggestions for autocomplete...";
  if (metadata.state === "error") return `Autocomplete metadata unavailable: ${metadata.error}`;
  if (metadata.state !== "ready") return "Run bounded read-only SQL through this credential profile.";
  if (metadata.tables.length === 0) return "No metadata suggestions found. Run bounded read-only SQL through this credential profile.";
  return `${metadata.tables.length} metadata suggestion${metadata.tables.length === 1 ? "" : "s"} loaded${metadata.truncated ? "; metadata limit reached" : ""}. Run bounded read-only ${config.label} SQL through this credential profile.`;
}

export function recentSQLQueries(items, config) {
  const seen = new Set();
  return [...(items || [])]
    .filter((item) => item?.action_name === config.queryAction && !isAutocompleteMetadataRequest(item, config.metadataReason))
    .sort((left, right) => safeTimestamp(right.created_at) - safeTimestamp(left.created_at))
    .map((item) => ({ id: item.id, sql: actionInputSQL(item), createdAt: item.created_at }))
    .filter((item) => rememberUniqueSQL(item, seen))
    .slice(0, 10)
    .map((item) => ({ ...item, preview: sqlPreview(item.sql) }));
}

function rememberUniqueSQL(item, seen) {
  if (!item.sql || seen.has(item.sql)) return false;
  seen.add(item.sql);
  return true;
}

export function actionInputSQL(item) {
  const input = typeof item?.input === "string" ? parseJSON(item.input) : item?.input;
  return String(input?.sql || "").trim();
}

export function tableBrowserSummary(metadata, rows, browserLabel) {
  if (metadata.state === "loading") return `Loading visible ${browserLabel.toLowerCase()}s and tables...`;
  if (metadata.state === "error") return `${browserLabel} metadata is unavailable. You can still run read-only SQL.`;
  if (rows.length === 0) return "No visible tables found for this profile.";
  return `${rows.length} visible table${rows.length === 1 ? "" : "s"}${metadata.truncated ? "; metadata limit reached" : ""}. Select one to prepare a read-only query.`;
}

export function mergeMetadataRows(current, incoming) {
  const merged = [];
  const seen = new Set();
  for (const item of [...(current || []), ...(incoming || [])]) {
    const key = [
      normalizeSQLName(item.schema),
      normalizeSQLName(item.table),
      normalizeSQLName(item.column),
      normalizeSQLName(item.dataType || item.type),
      item.position || "",
    ].join(".");
    if (seen.has(key)) continue;
    seen.add(key);
    merged.push(item);
  }
  return merged;
}

export function filteredTableBrowserRows(rows, search) {
  const terms = normalizeSQLName(search).split(/\s+/).filter(Boolean);
  const tables = uniqueTableBrowserRows(rows || []);
  if (terms.length === 0) return tables;
  return tables.filter((row) => terms.every((term) => normalizeSQLName(`${row.schema} ${row.table}`).includes(term)));
}

export function isAutocompleteMetadataRequest(item, metadataReason) {
  return item?.reason === metadataReason;
}

function uniqueTableBrowserRows(rows) {
  const byTable = new Map();
  const seenColumns = new Set();
  for (const row of rows) {
    if (!row.schema || !row.table) continue;
    const key = `${normalizeSQLName(row.schema)}.${normalizeSQLName(row.table)}`;
    const current = byTable.get(key) || { schema: row.schema, table: row.table, type: row.type || "table", columnCount: 0, columns: [] };
    if (row.column && !seenColumns.has(`${key}.${normalizeSQLName(row.column)}`)) {
      seenColumns.add(`${key}.${normalizeSQLName(row.column)}`);
      current.columns.push({ name: row.column, dataType: row.dataType || "", position: row.position || current.columns.length + 1 });
    }
    byTable.set(key, current);
  }
  return Array.from(byTable.values())
    .map((table) => ({
      ...table,
      columnCount: table.columns.length,
      columns: [...table.columns].sort((a, b) => (a.position || 0) - (b.position || 0) || a.name.localeCompare(b.name)),
    }))
    .sort((a, b) => a.schema.localeCompare(b.schema) || a.table.localeCompare(b.table));
}

function defaultTargetEndpoint(target, defaultPort, defaultDatabase) {
  if (!target) return "-";
  return `${target.config?.host || "host"}:${target.config?.port || defaultPort || "port"}/${target.config?.database || defaultDatabase}`;
}

function sqlPreview(sql) {
  const compact = String(sql || "")
    .replace(/\s+/g, " ")
    .trim();
  return compact.length <= 64 ? compact : `${compact.slice(0, 61)}...`;
}

function safeTimestamp(value) {
  const parsed = Date.parse(value || "");
  return Number.isFinite(parsed) ? parsed : 0;
}

function parseJSON(value) {
  try {
    return JSON.parse(value);
  } catch {
    return null;
  }
}

function quoteSQLIdentifier(value) {
  return /^[a-z_][a-z0-9_]*$/.test(value) ? value : `"${String(value).replaceAll('"', '""')}"`;
}

const DEFAULT_SQL_KEYWORDS = [
  "select",
  "from",
  "where",
  "join",
  "left join",
  "inner join",
  "group by",
  "order by",
  "limit",
  "with",
  "explain",
  "show",
  "count",
  "distinct",
  "having",
  "union",
  "case",
  "when",
  "then",
  "else",
  "end",
  "true",
  "false",
  "null",
];
