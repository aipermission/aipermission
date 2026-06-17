import { Download, RefreshCcw, Upload, UserPlus } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Dialog } from "../../../components/ui/dialog";
import { Checkbox, Field, Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { apiDownload, apiPost, apiPostForm } from "../../../lib/api";

const defaultProvisionForm = {
  role_name: "",
  profile_label: "",
  preset: "read_only",
};

const metadataSQL = `
SELECT
  n.nspname AS table_schema,
  c.relname AS table_name,
  COALESCE(
    json_agg(a.attname ORDER BY a.attnum) FILTER (WHERE a.attname IS NOT NULL),
    '[]'::json
  ) AS columns
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f')
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
GROUP BY n.nspname, c.relname
ORDER BY n.nspname, c.relname
`;

export function PostgresConnectorOperationsTemplate({ value, onChange, onOperationComplete }) {
  const operation = value?.connector_kind === "postgres" ? value : { open: false };

  function close() {
    onChange({ open: false, connector_kind: "", type: "", state: "idle", error: null });
  }

  return (
    <>
      <ProvisionUserDialog
        value={operation.type === "provision-user" ? operation : { open: false }}
        onClose={close}
        onOperationComplete={onOperationComplete}
      />
      <BackupRestoreDialog value={operation.type === "backup-restore" ? operation : { open: false }} onClose={close} />
    </>
  );
}

function ProvisionUserDialog({ value, onClose, onOperationComplete }) {
  const [form, setForm] = useState(defaultProvisionForm);
  const [metadata, setMetadata] = useState({ state: "idle", error: "", schemas: [] });
  const [scope, setScope] = useState({ all_schemas: true, schemas: {} });
  const [state, setState] = useState({ state: "idle", error: "", result: null });
  const targetRef = value.profile?.ref || "";
  const targetID = value.target?.id;
  const profileID = value.profile?.id;
  const selectedScope = useMemo(() => buildProvisionScope(scope), [scope]);
  const sqlPreview = useMemo(() => buildProvisionSQLPreview({
    roleName: form.role_name,
    preset: form.preset,
    database: value.target?.config?.database || "database",
    scope: selectedScope,
  }), [form.role_name, form.preset, selectedScope, value.target?.config?.database]);
  const scopeSummary = useMemo(() => readableScopeSummary(selectedScope, form.preset), [selectedScope, form.preset]);
  const canSubmit = Boolean(targetID && profileID && form.role_name.trim() && selectedScope);

  useEffect(() => {
    if (!value.open) return;
    setForm(defaultProvisionForm);
    setMetadata({ state: "idle", error: "", schemas: [] });
    setScope({ all_schemas: true, schemas: {} });
    setState({ state: "idle", error: "", result: null });
    if (targetRef) {
      void loadMetadata();
    }
  }, [value.open, targetRef]);

  function update(field, nextValue) {
    setForm((current) => ({ ...current, [field]: nextValue }));
  }

  async function loadMetadata() {
    if (!targetRef) return;
    setMetadata({ state: "loading", error: "", schemas: [] });
    try {
      const item = await apiPost("/api/connector-actions/local-run", {
        target_ref: targetRef,
        action_name: "query_readonly",
        input: { sql: metadataSQL, max_rows: 1000 },
        reason: "load Postgres schema metadata for managed credential provisioning",
      });
      setMetadata({ state: "ready", error: "", schemas: groupMetadataRows(item?.output?.rows || []) });
    } catch (error) {
      setMetadata({ state: "error", error: error.message || "Could not load schema metadata.", schemas: [] });
    }
  }

  async function provisionUser(event) {
    event.preventDefault();
    if (!canSubmit) return;
    setState({ state: "running", error: "", result: null });
    try {
      const result = await apiPost(`/api/connector-targets/${targetID}/profiles/${profileID}/provision`, {
        input: {
          role_name: form.role_name,
          profile_label: form.profile_label || form.role_name,
          preset: form.preset,
          scope: selectedScope,
        },
      });
      setState({ state: "ready", error: "", result });
      await onOperationComplete?.({ message: "Managed Postgres credential created." }, value);
    } catch (error) {
      setState({ state: "error", error: error.message || "Could not create managed Postgres credential.", result: null });
    }
  }

  return (
    <Dialog
      open={value.open}
      title={value.target ? `${value.target.name} managed DB user` : "Create managed DB user"}
      description="Create a scoped Postgres role with a random password and save it as an encrypted credential profile."
      onClose={onClose}
      size="xl"
      className="!w-[calc(100vw-300px)] !max-w-none h-[calc(100vh-200px)] grid-rows-[auto_minmax(0,1fr)]"
      bodyClassName="min-h-0 overflow-hidden"
    >
      <form className="grid h-full min-h-0 grid-rows-[auto_auto_minmax(0,1fr)_auto_auto] gap-4" onSubmit={provisionUser}>
        <Notice tone="warn">
          AIPermission will create the database role through this admin profile. When the managed credential is deleted, the managed database role is dropped too.
        </Notice>
        <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_180px]">
          <Field>
            Role name
            <Input value={form.role_name} onChange={(event) => update("role_name", event.target.value)} placeholder="app_reader" required />
          </Field>
          <Field>
            Profile label
            <Input value={form.profile_label} onChange={(event) => update("profile_label", event.target.value)} placeholder={form.role_name || "app_reader"} />
          </Field>
          <Field>
            Preset
            <Select value={form.preset} onChange={(event) => update("preset", event.target.value)}>
              <option value="read_only">Read only</option>
              <option value="read_write">Read and change</option>
            </Select>
          </Field>
        </div>

        <section className="grid min-h-0 gap-3 rounded-lg border border-stone-200 bg-stone-50 p-3 lg:grid-cols-2">
          <div className="grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] gap-3">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <h4 className="text-sm font-semibold text-stone-900">Access scope</h4>
                <p className="text-xs text-stone-500">Choose all schemas, or narrow the role to selected schemas, tables, and columns.</p>
              </div>
              <Button type="button" variant="outline" className="h-8 px-3 text-xs" onClick={loadMetadata} disabled={!targetRef || metadata.state === "loading"}>
                <RefreshCcw className="h-3.5 w-3.5" />
                {metadata.state === "loading" ? "Loading" : "Refresh"}
              </Button>
            </div>
            <label className="flex items-start gap-3 rounded-md border border-stone-200 bg-white p-3 text-sm">
              <Checkbox checked={scope.all_schemas} onChange={(event) => setScope({ ...scope, all_schemas: event.target.checked })} />
              <span>
                <span className="block font-semibold text-stone-900">All schemas, all tables, all columns</span>
                <span className="text-xs text-stone-500">Grant the preset across every non-system schema visible to the admin profile.</span>
              </span>
            </label>
            {!scope.all_schemas ? (
              <SchemaScopePicker metadata={metadata} scope={scope} onChange={setScope} preset={form.preset} />
            ) : (
              <div className="min-h-0" />
            )}
          </div>
          <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-3">
            <Notice tone="good" className="max-h-[200px] overflow-auto">{scopeSummary}</Notice>
            <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
              <div className="flex items-center justify-between gap-3">
                <h4 className="text-sm font-semibold text-stone-900">SQL preview</h4>
                <CopyButton value={sqlPreview} variant="outline" className="h-8 px-3 text-xs" title="Copy SQL preview" />
              </div>
              <TerminalBlock surface="log" className="min-h-0 text-xs">{sqlPreview}</TerminalBlock>
            </div>
          </div>
        </section>

        {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
        {metadata.state === "error" ? <Notice tone="bad">{metadata.error}</Notice> : null}
        {state.result ? (
          <Notice tone="good">
            {state.result.result?.display_text || "Managed Postgres credential created."} New profile: {state.result.profile?.label}
          </Notice>
        ) : null}

        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose}>
            Close
          </Button>
          <Button type="submit" disabled={!canSubmit || state.state === "running"}>
            <UserPlus className="h-4 w-4" />
            {state.state === "running" ? "Creating user" : "Create user"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function BackupRestoreDialog({ value, onClose }) {
  const [backupState, setBackupState] = useState({ state: "idle", error: "", message: "" });
  const [restoreState, setRestoreState] = useState({ state: "idle", error: "", message: "" });
  const [file, setFile] = useState(null);
  const [confirmTarget, setConfirmTarget] = useState("");
  const [activeTab, setActiveTab] = useState("backup");
  const targetID = value.target?.id;
  const profileID = value.profile?.id;
  const targetName = value.target?.name || "";
  const endpoint = targetID && profileID ? `/api/connector-targets/${targetID}/profiles/${profileID}` : "";
  const isWorking = backupState.state === "running" || restoreState.state === "running";
  const restoreReady = Boolean(endpoint && file && confirmTarget === targetName && !isWorking);

  useEffect(() => {
    if (!value.open) return;
    setBackupState({ state: "idle", error: "", message: "" });
    setRestoreState({ state: "idle", error: "", message: "" });
    setFile(null);
    setConfirmTarget("");
    setActiveTab("backup");
  }, [value.open, targetID, profileID]);

  async function downloadBackup() {
    if (!endpoint) return;
    setBackupState({ state: "running", error: "", message: "" });
    try {
      const result = await apiDownload(`${endpoint}/backup`, `${safeFilename(targetName || "postgres")}.sql`, { picker: true });
      if (result?.canceled) {
        setBackupState({ state: "idle", error: "", message: "" });
        return;
      }
      setBackupState({ state: "ready", error: "", message: "Backup downloaded as a restore-grade SQL dump." });
    } catch (error) {
      setBackupState({ state: "error", error: error.message || "Could not download backup.", message: "" });
    }
  }

  async function restoreBackup(event) {
    event.preventDefault();
    if (!restoreReady) return;
    setRestoreState({ state: "running", error: "", message: "" });
    try {
      const formData = new FormData();
      formData.append("dump", file);
      formData.append("confirm_target", confirmTarget);
      await apiPostForm(`${endpoint}/restore`, formData);
      setRestoreState({ state: "ready", error: "", message: "Restore completed." });
      setFile(null);
      setConfirmTarget("");
    } catch (error) {
      setRestoreState({ state: "error", error: error.message || "Could not restore backup.", message: "" });
    }
  }

  return (
    <Dialog
      open={value.open}
      title={value.target ? `${value.target.name} backup / restore` : "Backup / restore database"}
      description="Download a plain SQL dump, or restore an SQL dump into this Postgres target."
      onClose={onClose}
      size="xl"
      className="!max-w-3xl"
    >
      <div className="grid gap-5">
        <div className="inline-flex w-fit rounded-md border border-stone-200 bg-stone-50 p-1">
          <button
            type="button"
            className={`rounded px-3 py-1.5 text-sm font-medium ${activeTab === "backup" ? "bg-white text-stone-950 shadow-sm" : "text-stone-500 hover:text-stone-900"}`}
            onClick={() => setActiveTab("backup")}
          >
            Backup
          </button>
          <button
            type="button"
            className={`rounded px-3 py-1.5 text-sm font-medium ${activeTab === "restore" ? "bg-red-600 text-white shadow-sm" : "text-stone-500 hover:text-stone-900"}`}
            onClick={() => setActiveTab("restore")}
          >
            Restore
          </button>
        </div>

        {activeTab === "backup" ? (
          <section className="grid gap-3 rounded-lg border border-stone-200 bg-stone-50 p-3">
            <div>
              <h4 className="text-sm font-semibold text-stone-900">Backup</h4>
              <p className="text-xs text-stone-500">Creates a plain SQL dump with clean statements, no owner, and no privilege grants.</p>
            </div>
            <div className="flex justify-end">
              <Button type="button" onClick={downloadBackup} disabled={!endpoint || isWorking}>
                <Download className="h-4 w-4" />
                {backupState.state === "running" ? "Working" : "Download SQL dump"}
              </Button>
            </div>
          </section>
        ) : (
          <form className="grid gap-3 rounded-lg border border-red-200 bg-red-50 p-3 dark-notice-bad" onSubmit={restoreBackup}>
            <Notice tone="bad">
              Restore executes the selected SQL file against this database profile. Use a trusted dump and verify the target before continuing.
            </Notice>
            <div>
              <h4 className="text-sm font-semibold">Restore</h4>
              <p className="text-xs">This may drop and recreate objects if the dump contains clean statements. Type the connector target name exactly before restoring.</p>
            </div>
            <Field>
              SQL dump file
              <Input type="file" accept=".sql,text/plain,application/sql" onChange={(event) => setFile(event.target.files?.[0] || null)} />
            </Field>
            <Field>
              Type target name to confirm: {targetName}
              <Input value={confirmTarget} onChange={(event) => setConfirmTarget(event.target.value)} placeholder={targetName} autoComplete="off" />
            </Field>
            <div className="flex justify-end">
              <Button type="submit" variant="danger" disabled={!restoreReady}>
                <Upload className="h-4 w-4" />
                {restoreState.state === "running" ? "Restoring" : "Restore SQL dump"}
              </Button>
            </div>
          </form>
        )}
        {activeTab === "backup" && backupState.state === "error" ? <Notice tone="bad">{backupState.error}</Notice> : null}
        {activeTab === "backup" && backupState.message ? <Notice tone="good">{backupState.message}</Notice> : null}
        {activeTab === "restore" && restoreState.state === "error" ? <Notice tone="bad">{restoreState.error}</Notice> : null}
        {activeTab === "restore" && restoreState.message ? <Notice tone="good">{restoreState.message}</Notice> : null}
      </div>
    </Dialog>
  );
}

function SchemaScopePicker({ metadata, scope, onChange, preset }) {
  const schemas = metadata.schemas || [];
  if (metadata.state === "loading") {
    return <div className="rounded-md border border-dashed border-stone-300 bg-white p-4 text-sm text-stone-500">Loading schema metadata...</div>;
  }
  if (schemas.length === 0) {
    return <div className="rounded-md border border-dashed border-stone-300 bg-white p-4 text-sm text-stone-500">No schema metadata loaded. Refresh metadata or use all schemas.</div>;
  }
  return (
    <div className="h-full min-h-0 overflow-y-auto rounded-md border border-stone-200 bg-white">
      {schemas.map((schema) => {
        const schemaState = scope.schemas[schema.name] || { selected: false, all_tables: true, tables: {} };
        return (
          <div className="border-b border-stone-200 p-3 last:border-b-0" key={schema.name}>
            <label className="flex items-start gap-3 text-sm">
              <Checkbox checked={schemaState.selected} onChange={(event) => onChange(toggleSchema(scope, schema.name, event.target.checked))} />
              <span>
                <span className="block font-semibold text-stone-900">{schema.name}</span>
                <span className="text-xs text-stone-500">{schema.tables.length} tables</span>
              </span>
            </label>
            {schemaState.selected ? (
              <div className="mt-3 ml-7 grid gap-3">
                <label className="flex items-center gap-2 text-sm">
                  <Checkbox checked={schemaState.all_tables} onChange={(event) => onChange(updateSchema(scope, schema.name, { all_tables: event.target.checked }))} />
                  <span>All tables and columns in this schema</span>
                </label>
                {!schemaState.all_tables ? (
                  <div className="grid gap-2">
                    {schema.tables.map((table) => (
                      <TableScopeRow key={table.name} schema={schema.name} table={table} tableState={schemaState.tables[table.name]} scope={scope} onChange={onChange} preset={preset} />
                    ))}
                  </div>
                ) : null}
              </div>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}

function TableScopeRow({ schema, table, tableState, scope, onChange, preset }) {
  const current = tableState || { selected: false, all_columns: true, columns: {} };
  const columnScopedDisabled = preset === "read_write";
  return (
    <div className="rounded-md border border-stone-200 bg-stone-50 p-2">
      <label className="flex items-start gap-3 text-sm">
        <Checkbox checked={current.selected} onChange={(event) => onChange(toggleTable(scope, schema, table.name, event.target.checked))} />
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
              onChange={(event) => onChange(updateTable(scope, schema, table.name, { all_columns: event.target.checked }))}
            />
            <span>{columnScopedDisabled ? "All columns required for read and change preset" : "All columns"}</span>
          </label>
          {!current.all_columns && !columnScopedDisabled ? (
            <div className="grid gap-1">
              {table.columns.map((column) => (
                <label className="flex items-center gap-2 rounded border border-stone-200 bg-white px-2 py-1 text-xs" key={column}>
                  <Checkbox checked={Boolean(current.columns?.[column])} onChange={(event) => onChange(toggleColumn(scope, schema, table.name, column, event.target.checked))} />
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

function groupMetadataRows(rows) {
  const schemas = new Map();
  for (const row of rows || []) {
    const schemaName = String(row.table_schema || "").trim();
    const tableName = String(row.table_name || "").trim();
    const columns = metadataColumns(row);
    if (!schemaName || !tableName) continue;
    if (!schemas.has(schemaName)) {
      schemas.set(schemaName, new Map());
    }
    const tables = schemas.get(schemaName);
    if (!tables.has(tableName)) tables.set(tableName, []);
    if (columns.length > 0) {
      tables.set(tableName, uniqueStrings([...tables.get(tableName), ...columns]));
    }
  }
  return [...schemas.entries()].map(([name, tables]) => ({
    name,
    tables: [...tables.entries()].map(([tableName, columns]) => ({ name: tableName, columns })),
  }));
}

function metadataColumns(row) {
  if (Array.isArray(row.columns)) return row.columns.map((item) => String(item || "").trim()).filter(Boolean);
  if (typeof row.columns === "string" && row.columns.trim()) {
    try {
      const parsed = JSON.parse(row.columns);
      if (Array.isArray(parsed)) return parsed.map((item) => String(item || "").trim()).filter(Boolean);
    } catch {
      return row.columns.split(",").map((item) => item.trim()).filter(Boolean);
    }
  }
  const columnName = String(row.column_name || "").trim();
  return columnName ? [columnName] : [];
}

function uniqueStrings(items) {
  return [...new Set(items.filter(Boolean))];
}

function buildProvisionScope(scope) {
  if (scope.all_schemas) return { all_schemas: true };
  const schemas = Object.entries(scope.schemas || {})
    .filter(([, schema]) => schema.selected)
    .map(([schemaName, schema]) => {
      if (schema.all_tables) {
        return { schema: schemaName, all_tables: true };
      }
      const tables = Object.entries(schema.tables || {})
        .filter(([, table]) => table.selected)
        .map(([tableName, table]) => {
          if (table.all_columns) return { table: tableName, all_columns: true };
          return { table: tableName, all_columns: false, columns: Object.keys(table.columns || {}).filter((column) => table.columns[column]) };
        })
        .filter((table) => table.all_columns || table.columns.length > 0);
      return { schema: schemaName, all_tables: false, tables };
    })
    .filter((schema) => schema.all_tables || schema.tables.length > 0);
  if (schemas.length === 0) return null;
  return { all_schemas: false, schemas };
}

function toggleSchema(scope, schemaName, selected) {
  return updateScopeSchema(scope, schemaName, (current) => ({ ...current, selected, all_tables: current.all_tables ?? true }));
}

function updateSchema(scope, schemaName, patch) {
  return updateScopeSchema(scope, schemaName, (current) => ({ ...current, ...patch }));
}

function toggleTable(scope, schemaName, tableName, selected) {
  return updateScopeTable(scope, schemaName, tableName, (current) => ({ ...current, selected, all_columns: current.all_columns ?? true }));
}

function updateTable(scope, schemaName, tableName, patch) {
  return updateScopeTable(scope, schemaName, tableName, (current) => ({ ...current, ...patch }));
}

function toggleColumn(scope, schemaName, tableName, columnName, selected) {
  return updateScopeTable(scope, schemaName, tableName, (current) => ({
    ...current,
    columns: { ...(current.columns || {}), [columnName]: selected },
  }));
}

function updateScopeSchema(scope, schemaName, updater) {
  const current = scope.schemas?.[schemaName] || { selected: false, all_tables: true, tables: {} };
  return {
    ...scope,
    schemas: {
      ...(scope.schemas || {}),
      [schemaName]: updater(current),
    },
  };
}

function updateScopeTable(scope, schemaName, tableName, updater) {
  return updateScopeSchema(scope, schemaName, (schema) => {
    const current = schema.tables?.[tableName] || { selected: false, all_columns: true, columns: {} };
    return {
      ...schema,
      tables: {
        ...(schema.tables || {}),
        [tableName]: updater(current),
      },
    };
  });
}

function readableScopeSummary(scope, preset) {
  const privilege = preset === "read_write" ? "read and change rows" : "read rows";
  if (!scope) return "Choose at least one schema/table scope before creating the managed credential.";
  if (scope.all_schemas) {
    return `The generated role can ${privilege} across all non-system schemas visible to the admin profile.`;
  }
  const schemaCount = scope.schemas?.length || 0;
  const tableCount = (scope.schemas || []).reduce((sum, schema) => sum + (schema.all_tables ? 0 : schema.tables.length), 0);
  const allTableSchemas = (scope.schemas || []).filter((schema) => schema.all_tables).length;
  if (allTableSchemas === schemaCount) {
    return `The generated role can ${privilege} across all tables in ${schemaCount} selected schema${schemaCount === 1 ? "" : "s"}.`;
  }
  return `The generated role can ${privilege} on ${tableCount} selected table${tableCount === 1 ? "" : "s"} across ${schemaCount} schema${schemaCount === 1 ? "" : "s"}.`;
}

function buildProvisionSQLPreview({ roleName, preset, database, scope }) {
  const cleanRole = cleanPreviewIdentifier(roleName) || "role_name";
  const cleanDatabase = cleanPreviewIdentifier(database) || "database";
  const role = quotePreviewIdentifier(cleanRole);
  const privileges = preset === "read_write" ? "SELECT, INSERT, UPDATE, DELETE" : "SELECT";
  const lines = [
    `CREATE ROLE ${role} LOGIN PASSWORD '<random password generated by AIPermission>';`,
    `GRANT CONNECT ON DATABASE ${quotePreviewIdentifier(cleanDatabase)} TO ${role};`,
  ];
  if (!scope) {
    lines.push("-- Choose a scope to preview grants.");
    return lines.join("\n");
  }
  if (scope.all_schemas) {
    lines.push("-- For every non-system schema visible to the admin profile:");
    lines.push(`GRANT USAGE ON SCHEMA <schema> TO ${role};`);
    lines.push(`GRANT ${privileges} ON ALL TABLES IN SCHEMA <schema> TO ${role};`);
    return lines.join("\n");
  }
  for (const schema of scope.schemas || []) {
    const schemaSQL = quotePreviewIdentifier(schema.schema);
    lines.push(`GRANT USAGE ON SCHEMA ${schemaSQL} TO ${role};`);
    if (schema.all_tables) {
      lines.push(`GRANT ${privileges} ON ALL TABLES IN SCHEMA ${schemaSQL} TO ${role};`);
      continue;
    }
    for (const table of schema.tables || []) {
      const tableSQL = `${schemaSQL}.${quotePreviewIdentifier(table.table)}`;
      if (table.all_columns) {
        lines.push(`GRANT ${privileges} ON TABLE ${tableSQL} TO ${role};`);
      } else {
        const columns = (table.columns || []).map(quotePreviewIdentifier).join(", ");
        lines.push(`GRANT SELECT (${columns}) ON TABLE ${tableSQL} TO ${role};`);
      }
    }
  }
  return lines.join("\n");
}

function cleanPreviewIdentifier(value) {
  const text = String(value || "").trim();
  return /^[A-Za-z_][A-Za-z0-9_]*$/.test(text) ? text : "";
}

function quotePreviewIdentifier(value) {
  return `"${String(value || "").replaceAll('"', '""')}"`;
}

function safeFilename(value) {
  const text = String(value || "").trim().toLowerCase().replace(/[^a-z0-9._-]+/g, "-").replace(/^-+|-+$/g, "");
  return text || "postgres-backup";
}
