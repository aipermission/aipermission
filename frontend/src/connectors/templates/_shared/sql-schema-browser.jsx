import { ChevronDown, ChevronRight, Database } from "lucide-react";
import { useState } from "react";
import { Button } from "../../../components/ui/button";
import { Notice } from "../../../components/ui/notice";
import { normalizeSQLName } from "./sql-console-data";

export function SQLSchemaBrowser({
  rows,
  search,
  onSearch,
  onPrepareQuery,
  metadata,
  theme,
  inputClass,
  mutedClass,
  hoverClass,
  namespaceLabel,
}) {
  const [expandedTables, setExpandedTables] = useState({});
  const grouped = groupTableBrowserRows(rows);
  return (
    <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2 p-3">
      <input
        type="search"
        className={`h-9 rounded-md border px-3 text-sm outline-none ${inputClass}`}
        value={search}
        onChange={(event) => onSearch(event.target.value)}
        placeholder={`Search ${namespaceLabel.toLowerCase()}s or tables`}
        aria-label={`Search ${namespaceLabel.toLowerCase()}s or tables`}
      />
      <div className="min-h-0 overflow-y-auto">
        {metadata.state === "loading" ? (
          <p className={`px-1 py-3 text-sm ${mutedClass}`}>Loading {namespaceLabel.toLowerCase()} metadata...</p>
        ) : null}
        {metadata.state === "error" ? (
          <Notice tone="bad">{metadata.error || `${namespaceLabel} metadata could not be loaded.`}</Notice>
        ) : null}
        {metadata.state !== "loading" && grouped.length === 0 ? (
          <p className={`px-1 py-3 text-sm ${mutedClass}`}>No tables found for this profile.</p>
        ) : null}
        {grouped.map((group) => (
          <SchemaGroup
            key={group.schema}
            group={group}
            expandedTables={expandedTables}
            setExpandedTables={setExpandedTables}
            theme={theme}
            mutedClass={mutedClass}
            hoverClass={hoverClass}
            onPrepareQuery={onPrepareQuery}
          />
        ))}
      </div>
    </div>
  );
}

function SchemaGroup({ group, expandedTables, setExpandedTables, theme, mutedClass, hoverClass, onPrepareQuery }) {
  return (
    <div className="mb-3">
      <p className={`mb-1 truncate px-1 text-[11px] font-semibold uppercase tracking-wide ${mutedClass}`}>{group.schema}</p>
      <div className={`overflow-hidden rounded-md border ${theme === "light" ? "border-stone-200" : "border-stone-700"}`}>
        {group.tables.map((table) => (
          <TableRow
            key={tableBrowserKey(table)}
            table={table}
            expanded={Boolean(expandedTables[tableBrowserKey(table)])}
            onToggle={() => setExpandedTables((current) => ({ ...current, [tableBrowserKey(table)]: !current[tableBrowserKey(table)] }))}
            theme={theme}
            mutedClass={mutedClass}
            hoverClass={hoverClass}
            onPrepareQuery={onPrepareQuery}
          />
        ))}
      </div>
    </div>
  );
}

function TableRow({ table, expanded, onToggle, theme, mutedClass, hoverClass, onPrepareQuery }) {
  const key = tableBrowserKey(table);
  const columns = table.columns || [];
  return (
    <div className={`border-b last:border-b-0 ${theme === "light" ? "border-stone-100" : "border-stone-800"}`}>
      <div className={`grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2 px-2 py-1.5 transition ${hoverClass}`}>
        <button
          type="button"
          aria-expanded={expanded}
          className="flex min-w-0 items-start gap-2 text-left"
          onClick={onToggle}
          title={`${expanded ? "Hide" : "Show"} columns for ${table.schema}.${table.table}`}
        >
          <span className="mt-0.5 shrink-0">
            {expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
          </span>
          <span className="min-w-0">
            <span className="block truncate font-mono text-xs font-semibold">{table.table}</span>
            <span className={`block truncate text-[11px] ${mutedClass}`}>
              {table.columnCount} column{table.columnCount === 1 ? "" : "s"}
            </span>
          </span>
        </button>
        <Button
          type="button"
          variant="outline"
          className="h-7 w-7 px-0"
          title={`Prepare SELECT query for ${table.schema}.${table.table}`}
          aria-label={`Prepare SELECT query for ${table.schema}.${table.table}`}
          onClick={() => onPrepareQuery(table)}
        >
          <Database className="h-3.5 w-3.5" />
        </Button>
      </div>
      {expanded ? (
        <div className={`grid gap-1 px-8 pb-2 text-[11px] ${mutedClass}`}>
          {columns.length > 0 ? (
            columns.map((column) => (
              <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-2 rounded px-2 py-1 font-mono" key={`${key}.${column.name}`}>
                <span className="truncate">{column.name}</span>
                {column.dataType ? <span className="truncate opacity-75">{column.dataType}</span> : null}
              </div>
            ))
          ) : (
            <span className="rounded px-2 py-1">No column metadata loaded.</span>
          )}
        </div>
      ) : null}
    </div>
  );
}

function tableBrowserKey(table) {
  return `${normalizeSQLName(table.schema)}.${normalizeSQLName(table.table)}`;
}

function groupTableBrowserRows(rows) {
  const bySchema = new Map();
  for (const row of rows) {
    const schema = row.schema || "public";
    const group = bySchema.get(schema) || { schema, tables: [] };
    group.tables.push(row);
    bySchema.set(schema, group);
  }
  return Array.from(bySchema.values());
}
