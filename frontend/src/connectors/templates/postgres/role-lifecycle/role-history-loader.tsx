import { lazy, Suspense, useState } from "react";
import type { ComponentProps } from "react";
import { Dialog } from "../../../../components/ui/dialog";
import { currentWorkspaceBinding } from "../../../../lib/api";
import type { RoleHistoryDialog } from "./role-history-dialog";

const HistoryDialog = lazy(() => import("./role-history-dialog").then((module) => ({ default: module.RoleHistoryDialog })));

export function RoleHistoryLoader(props: Omit<ComponentProps<typeof RoleHistoryDialog>, "workspaceBinding">) {
  const [workspaceBinding] = useState(currentWorkspaceBinding);
  return (
    <Suspense
      fallback={
        <Dialog open title="Role history" onClose={props.onClose}>
          <div role="status">Loading role history...</div>
        </Dialog>
      }
    >
      <HistoryDialog {...props} workspaceBinding={workspaceBinding} />
    </Suspense>
  );
}
