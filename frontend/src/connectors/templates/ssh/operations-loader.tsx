import { lazy, Suspense, useEffect, useState } from "react";
import { Dialog } from "../../../components/ui/dialog";
import { currentWorkspaceBinding } from "../../../lib/api";
import type { SSHOperationProps } from "./operation-types";

const Operations = lazy(() => import("./operations").then((module) => ({ default: module.SSHConnectorOperationsTemplate })));

export function SSHConnectorOperationsLoader(props: SSHOperationProps) {
  if (!props.value?.open || props.value.connector_kind !== "ssh") return null;
  return <WorkspaceOperation {...props} />;
}

function WorkspaceOperation(props: SSHOperationProps) {
  const [workspace] = useState(currentWorkspaceBinding);
  return (
    <Suspense
      fallback={
        <Dialog open title="Connector operation" onClose={() => props.onChange({ open: false })}>
          <div role="status">Loading operation...</div>
        </Dialog>
      }
    >
      <WorkspaceOperations workspace={workspace} {...props} />
    </Suspense>
  );
}

function WorkspaceOperations({ workspace, ...props }: SSHOperationProps & { workspace: string }) {
  const current = currentWorkspaceBinding();
  const { onChange } = props;
  useEffect(() => {
    if (current !== workspace) onChange({ open: false });
  }, [current, workspace, onChange]);
  if (current !== workspace) return null;
  return <Operations {...props} />;
}
