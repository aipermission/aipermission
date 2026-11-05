import { useLayoutEffect, useMemo, useState } from "react";
import { currentWorkspaceBinding } from "../../../lib/api";
import { runGuardedConnectorAction } from "./action-runner";
import { useConnectorMutationOwnership } from "./use-connector-mutation-ownership";
import type { GuardedConnectorActionOptions } from "./action-runner";

type ExecutionOptions = Pick<GuardedConnectorActionOptions, "requestGuard" | "targetRef" | "product" | "setState" | "onRefreshActivity"> & {
  scope: string;
  actionName?: string;
  approvalState?: string;
  onSelected: (_id: number) => void;
};

export function useSQLExecution({ scope, actionName, approvalState, onSelected, ...options }: ExecutionOptions) {
  const actions = useMemo(() => (actionName ? [actionName] : []), [actionName]);
  const owner = useConnectorMutationOwnership(options.targetRef, actions, actionName ? approvalState : undefined);
  const [mode, setMode] = useState({ scope, execute: false });
  const [timeout, setTimeout] = useState<{ scope: string; value: number | string }>({ scope, value: 30 });
  const executionMode = Boolean(actionName && mode.scope === scope && mode.execute);
  const timeoutSeconds = timeout.scope === scope ? timeout.value : 30;
  useLayoutEffect(() => {
    setMode({ scope, execute: false });
    setTimeout({ scope, value: 30 });
  }, [scope]);
  const owned = owner.ownerRef.current?.scope === `${currentWorkspaceBinding()}:${options.targetRef}` ? owner.ownerRef.current : null;
  const unresolved = owner.unresolved?.target_ref === options.targetRef ? owner.unresolved : null;
  const requestID = unresolved?.id || owned?.requestID;
  const executionStatus =
    !actionName || !owner.locked
      ? ""
      : requestID
        ? `SQL execution request #${requestID} is pending or its outcome is unknown. Do not rerun the script.`
        : owned
          ? "SQL execution is in flight or its outcome is unconfirmed. Do not rerun the script."
          : "SQL execution is locked while checking for unresolved requests.";

  async function runExecution(sql: string, maxRows: number | string) {
    if (!executionMode || !actionName || owner.locked) return;
    const seconds = Number(timeoutSeconds);
    const rows = Number(maxRows);
    if (!Number.isInteger(seconds) || seconds < 1 || seconds > 300 || !Number.isInteger(rows) || rows < 1 || rows > 1000) {
      options.setState({ state: "error", error: "Use a timeout of 1-300 seconds and a returned-row cap of 1-1000.", message: "" });
      return;
    }
    try {
      await owner.run((onPending) =>
        runGuardedConnectorAction({
          ...options,
          actionName,
          channel: "query",
          input: { sql, max_rows: rows, timeout_seconds: seconds },
          reason: `manual ${options.product} console SQL execution`,
          exclusiveMutationActions: actions,
          onPending: (item) => {
            onPending(item);
            onSelected(item.request_id);
          },
          onCompleted: (item) => {
            onSelected(item.request_id);
          },
        }),
      );
    } catch {
      // The runner reports errors; ownership and the durable ledger retain uncertain attempts.
    }
  }

  return {
    executionMode,
    setExecutionMode: (execute: boolean) => setMode({ scope, execute }),
    timeoutSeconds,
    setTimeoutSeconds: (value: number | string) => setTimeout({ scope, value }),
    executionLocked: owner.locked,
    executionStatus,
    runExecution,
  };
}
