import { Notice } from "../../../components/ui/notice";
import { connectorActionMaximumExecutionSeconds } from "../../../lib/gateway-contracts/generated-connector-contract";
import type { SQLConsoleViewProps } from "./sql-console-view-types";

type ExecutionController = Pick<
  SQLConsoleViewProps["controller"],
  "connector" | "executionMode" | "setExecutionMode" | "timeoutSeconds" | "setTimeoutSeconds" | "executionStatus"
>;

export function SQLExecutionControls({
  controller,
  styles,
  running,
}: {
  controller: ExecutionController;
  styles: SQLConsoleViewProps["styles"];
  running: boolean;
}) {
  if (!controller.connector.executionAction) return null;
  return (
    <div className="grid min-w-0 gap-2">
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
        <fieldset className={`flex flex-wrap gap-1 rounded-md border p-1 text-xs ${styles.border}`} disabled={running}>
          <legend className="sr-only">SQL mode</legend>
          {[
            { execute: false, label: "Read-only" },
            { execute: true, label: "Execute SQL" },
          ].map(({ execute, label }) => (
            <label
              key={label}
              className={`flex cursor-pointer items-center gap-2 rounded px-2 py-1 ${controller.executionMode === execute ? styles.panel : styles.muted}`}
            >
              <input
                type="radio"
                name="sql-mode"
                checked={controller.executionMode === execute}
                onChange={() => controller.setExecutionMode(execute)}
              />
              {label}
            </label>
          ))}
        </fieldset>
        {controller.executionMode ? (
          <label className="flex items-center gap-2 text-xs font-semibold">
            Timeout (seconds)
            <input
              type="number"
              min="1"
              max={connectorActionMaximumExecutionSeconds}
              step="1"
              className={`h-8 w-20 rounded-md border px-2 outline-none ${styles.input}`}
              value={controller.timeoutSeconds}
              onChange={(event) => controller.setTimeoutSeconds(event.target.value)}
              disabled={running}
            />
          </label>
        ) : null}
      </div>
      {controller.executionMode ? (
        <Notice tone="warn">
          Write/admin SQL can permanently change or delete data. Row caps limit returned data, not changes. Transactions are script-defined;
          scripts are never retried automatically.
        </Notice>
      ) : null}
      {controller.executionMode && controller.executionStatus ? <Notice tone="warn">{controller.executionStatus}</Notice> : null}
    </div>
  );
}
