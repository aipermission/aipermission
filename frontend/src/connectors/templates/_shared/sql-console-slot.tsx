import type { ConsoleWorkspaceSlotProps } from "../../../components/console/console-workspace-types";
import type { SQLConsoleConfigInput } from "./sql-console-config";
import { SQLConnectorConsole } from "./sql-console";
import { structuredConsoleSlotSession } from "./console-slot-session";
import { optionalConsoleNumber, optionalConsoleText } from "./console-target-config";

export function createSQLConsoleSlot(config: SQLConsoleConfigInput) {
  return function SQLConsoleSlot(props: ConsoleWorkspaceSlotProps) {
    const targetConfig = props.target.config || {};
    const host = optionalConsoleText(targetConfig.host, "SQL", "host");
    const database = optionalConsoleText(targetConfig.database, "SQL", "database");
    const port = optionalConsoleNumber(targetConfig.port, "SQL", "port");
    const target = {
      ref: props.target.ref,
      name: props.target.name || props.target.target_name || config.label || "SQL",
      config: { host, port, database },
    };
    return (
      <SQLConnectorConsole
        config={config}
        target={target}
        approvals={props.approvals}
        session={structuredConsoleSlotSession(props.session)}
        theme={props.theme}
        onNewStructuredSession={props.onNewStructuredSession}
        onRefreshActivity={props.onRefreshActivity}
      />
    );
  };
}
