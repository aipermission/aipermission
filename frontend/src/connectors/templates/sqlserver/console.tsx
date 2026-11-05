import { SQLConnectorToolbarActions } from "../_shared/sql-console";
import { createSQLConsoleSlot } from "../_shared/sql-console-slot";
import type { ComponentProps } from "react";
import type { SQLConsoleConfigInput } from "../_shared/sql-console-config";

export const sqlServerConsoleConfig = {
  label: "SQL Server",
  queryAction: "query_readonly",
  executionAction: "execute_sql",
  describeAction: "describe_table",
  metadataAction: "get_metadata",
  metadataInput: {},
  metadataReason: "load SQL Server console autocomplete",
  manualReason: "manual SQL Server console query",
  browserLabel: "Schema",
  filenamePrefix: "sqlserver-result",
  defaultPort: 1433,
  identifierPolicy: "exact",
  keywords: [
    "top",
    "nvarchar",
    "dateadd",
    "datediff",
    "isnull",
    "count_big",
    "exec",
    "execute",
    "go",
    "create procedure",
    "alter procedure",
  ],
  describeInput: (reference) => ({ schema: reference.schema || "dbo", table: reference.table }),
  tableQuery: (table, maxRows) => `SELECT TOP (${maxRows}) *\nFROM ${quoteIdentifier(table.schema)}.${quoteIdentifier(table.table)};`,
} satisfies SQLConsoleConfigInput;

export const SQLServerConnectorConsoleTemplate = createSQLConsoleSlot(sqlServerConsoleConfig);
export function SQLServerConnectorToolbarActionsTemplate(props: Omit<ComponentProps<typeof SQLConnectorToolbarActions>, "label">) {
  return <SQLConnectorToolbarActions {...props} label="SQL Server" />;
}
function quoteIdentifier(value: string): string {
  return `[${value.replaceAll("]", "]]")}]`;
}
