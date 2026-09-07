import { Checkbox } from "../../../components/ui/form";
import { toggleColumn, toggleSchema, toggleTable, updateSchema, updateTable } from "./provisioning";

export function ProvisionScopePicker({ metadata, scope, onChange, preset }) {
  const schemas = metadata.schemas || [];
  if (metadata.state === "loading") {
    return <EmptyScopeState>Loading schema metadata...</EmptyScopeState>;
  }
  if (schemas.length === 0) {
    return <EmptyScopeState>No schema metadata loaded. Refresh metadata or use all schemas.</EmptyScopeState>;
  }
  return (
    <div className="h-full min-h-0 overflow-y-auto rounded-md border border-stone-200 bg-white">
      {schemas.map((schema) => (
        <SchemaScopeRow key={schema.name} schema={schema} scope={scope} onChange={onChange} preset={preset} />
      ))}
    </div>
  );
}

function EmptyScopeState({ children }) {
  return <div className="rounded-md border border-dashed border-stone-300 bg-white p-4 text-sm text-stone-500">{children}</div>;
}

function SchemaScopeRow({ schema, scope, onChange, preset }) {
  const schemaState = scope.schemas[schema.name] || { selected: false, all_tables: true, tables: {} };
  return (
    <div className="border-b border-stone-200 p-3 last:border-b-0">
      <label className="flex items-start gap-3 text-sm">
        <Checkbox
          checked={schemaState.selected}
          onChange={(event) => onChange((current) => toggleSchema(current, schema.name, event.target.checked))}
        />
        <span>
          <span className="block font-semibold text-stone-900">{schema.name}</span>
          <span className="text-xs text-stone-500">{schema.tables.length} tables</span>
        </span>
      </label>
      {schemaState.selected ? (
        <div className="mt-3 ml-7 grid gap-3">
          <label className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={schemaState.all_tables}
              onChange={(event) => onChange((current) => updateSchema(current, schema.name, { all_tables: event.target.checked }))}
            />
            <span>All tables and columns in this schema</span>
          </label>
          {!schemaState.all_tables ? (
            <div className="grid gap-2">
              {schema.tables.map((table) => (
                <TableScopeRow
                  key={table.name}
                  schema={schema.name}
                  table={table}
                  tableState={schemaState.tables[table.name]}
                  onChange={onChange}
                  preset={preset}
                />
              ))}
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function TableScopeRow({ schema, table, tableState, onChange, preset }) {
  const current = tableState || { selected: false, all_columns: true, columns: {} };
  const columnScopedDisabled = preset === "read_write";
  return (
    <div className="rounded-md border border-stone-200 bg-stone-50 p-2">
      <label className="flex items-start gap-3 text-sm">
        <Checkbox
          checked={current.selected}
          onChange={(event) => onChange((scope) => toggleTable(scope, schema, table.name, event.target.checked))}
        />
        <span>
          <span className="block font-semibold text-stone-900">{table.name}</span>
          <span className="text-xs text-stone-500">{table.columns.length} columns</span>
        </span>
      </label>
      {current.selected ? (
        <div className="mt-2 ml-7 grid gap-2">
          <label className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={current.all_columns || columnScopedDisabled}
              disabled={columnScopedDisabled}
              onChange={(event) => onChange((scope) => updateTable(scope, schema, table.name, { all_columns: event.target.checked }))}
            />
            <span>{columnScopedDisabled ? "All columns required for read and change preset" : "All columns"}</span>
          </label>
          {!current.all_columns && !columnScopedDisabled ? (
            <div className="grid gap-1">
              {table.columns.map((column) => (
                <label className="flex items-center gap-2 rounded border border-stone-200 bg-white px-2 py-1 text-xs" key={column}>
                  <Checkbox
                    checked={Boolean(current.columns?.[column])}
                    onChange={(event) => onChange((scope) => toggleColumn(scope, schema, table.name, column, event.target.checked))}
                  />
                  <span className="truncate">{column}</span>
                </label>
              ))}
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
