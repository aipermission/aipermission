import { SQLConnectorConsole, SQLConnectorToolbarActions } from "../_shared/sql-console";

const consoleMetadataSQL = `
SELECT
  n.nspname AS table_schema,
  c.relname AS table_name,
  c.relkind AS table_type,
  COALESCE(
    json_agg(
      json_build_object(
        'name', a.attname,
        'data_type', format_type(a.atttypid, a.atttypmod),
        'position', a.attnum
      )
      ORDER BY a.attnum
    ) FILTER (WHERE a.attname IS NOT NULL),
    '[]'::json
  ) AS columns
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f')
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND (
    has_table_privilege(c.oid, 'SELECT')
    OR has_table_privilege(c.oid, 'INSERT')
    OR has_table_privilege(c.oid, 'UPDATE')
    OR has_table_privilege(c.oid, 'DELETE')
  )
GROUP BY n.nspname, c.relname, c.relkind
ORDER BY n.nspname, c.relname
`;

const config = {
  label: "Postgres",
  queryAction: "query_readonly",
  describeAction: "describe_table",
  metadataSQL: consoleMetadataSQL,
  metadataReason: "load Postgres console autocomplete",
  manualReason: "manual Postgres console query",
  browserLabel: "Schema",
  filenamePrefix: "postgres-result",
  defaultPort: 5432,
  defaultDatabase: "database",
};

export function PostgresConnectorConsoleTemplate(props) {
  return <SQLConnectorConsole {...props} config={config} />;
}

export function PostgresConnectorToolbarActionsTemplate(props) {
  return <SQLConnectorToolbarActions {...props} label="Postgres" />;
}
