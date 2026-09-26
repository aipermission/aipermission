import type { ComponentType } from "react";
import type { ConsoleToolbarSlotProps, ConsoleWorkspaceSlotProps } from "../../components/console/console-workspace-types";

export interface ConsoleTemplateContract {
  Console: ComponentType<ConsoleWorkspaceSlotProps>;
  ToolbarActions?: ComponentType<ConsoleToolbarSlotProps>;
  [slot: string]: unknown;
}
