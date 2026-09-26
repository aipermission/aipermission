import { useEffect, useMemo, useState } from "react";
import { recoverableRunningActions } from "./console-target-sidebar";

type Target = { connector_kind: string; ref: string };
type Approval = { id?: number; status: string; target_ref: string; action_name: string };

export function useConsoleRecoveryState<ApprovalItem extends Approval>({
  approvals,
  selectedTarget,
  tickInterval = 5000,
}: {
  approvals: ApprovalItem[];
  selectedTarget: Target | null;
  tickInterval?: number;
}) {
  const [now, setNow] = useState(Date.now());
  const runningRequest = useMemo(() => selectedRecoverableRequest(approvals, selectedTarget), [approvals, selectedTarget]);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), tickInterval);
    return () => window.clearInterval(timer);
  }, [tickInterval]);

  return { now, runningRequest };
}

export function selectedRecoverableRequest<ApprovalItem extends Approval>(
  approvals: ApprovalItem[],
  selectedTarget: Target | null,
): ApprovalItem | null {
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
