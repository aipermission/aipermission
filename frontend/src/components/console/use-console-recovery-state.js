import { useEffect, useMemo, useState } from "react";
import { recoverableRunningActions } from "./console-target-sidebar";

export function useConsoleRecoveryState({ approvals, selectedTarget, tickInterval = 5000 }) {
  const [now, setNow] = useState(Date.now());
  const runningRequest = useMemo(() => selectedRecoverableRequest(approvals, selectedTarget), [approvals, selectedTarget]);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), tickInterval);
    return () => window.clearInterval(timer);
  }, [tickInterval]);

  return { now, runningRequest };
}

export function selectedRecoverableRequest(approvals, selectedTarget) {
  if (!selectedTarget) return null;
  const actionNames = recoverableRunningActions(selectedTarget);
  if (actionNames.length === 0) return null;
  return (
    approvals.find(
      (approval) =>
        approval.status === "running" && approval.target_ref === selectedTarget.ref && actionNames.includes(approval.action_name),
    ) || null
  );
}
