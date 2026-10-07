import { SQLConnectorToolbarActions } from "../_shared/sql-console";
import { createSQLConsoleSlot } from "../_shared/sql-console-slot";
import type { ComponentProps } from "react";
import type { SQLConsoleConfigInput } from "../_shared/sql-console-config";

const config: SQLConsoleConfigInput = {
  label: "Postgres",
  queryAction: "query_readonly",
  describeAction: "describe_table",
  metadataAction: "get_tables",
  metadataInput: {},
  metadataReason: "load Postgres console autocomplete",
  manualReason: "manual Postgres console query",
  browserLabel: "Schema",
  filenamePrefix: "postgres-result",
  defaultPort: 5432,
  defaultDatabase: "database",
};

export const PostgresConnectorConsoleTemplate = createSQLConsoleSlot(config);

export function PostgresConnectorToolbarActionsTemplate(props: Omit<ComponentProps<typeof SQLConnectorToolbarActions>, "label">) {
  return <SQLConnectorToolbarActions {...props} label="Postgres" />;
}
