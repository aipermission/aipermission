import { SQLConnectorConsole, SQLConnectorToolbarActions } from "../_shared/sql-console";

const consoleMetadataSQL = `
SELECT
  database,
  table AS table_name,
  toJSONString(arraySort(item -> item.1, groupArray((position, name, type)))) AS columns
FROM system.columns
WHERE database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')
GROUP BY database, table
ORDER BY database, table
`;

const config = {
  label: "ClickHouse",
  queryAction: "query_readonly",
  describeAction: "describe_table",
  metadataSQL: consoleMetadataSQL,
  metadataMaxRows: 1000,
  metadataReason: "load ClickHouse console autocomplete",
  manualReason: "manual ClickHouse console query",
  browserLabel: "Database",
  filenamePrefix: "clickhouse-result",
  defaultPort: 9000,
  defaultDatabase: "default",
  keywords: ["prewhere", "sample", "final", "settings", "global", "array", "tuple"],
  describeInput: (reference) => ({ database: reference.schema || "", table: reference.table }),
  tableQuery: (table, maxRows) =>
    `SELECT *\nFROM ${quoteClickHouseIdentifier(table.schema)}.${quoteClickHouseIdentifier(table.table)}\nLIMIT ${maxRows};`,
};

export function ClickHouseConnectorConsoleTemplate(props) {
  return <SQLConnectorConsole {...props} config={config} />;
}

export function ClickHouseConnectorToolbarActionsTemplate(props) {
  return <SQLConnectorToolbarActions {...props} label="ClickHouse" />;
}

function quoteClickHouseIdentifier(value) {
  return `\`${String(value || "").replaceAll("`", "``")}\``;
}
