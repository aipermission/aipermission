import type { ComponentProps, ComponentType, ReactNode } from "react";
import type { GatewayTarget } from "../../lib/gateway-contracts/core-resource-contracts";
import type { ConnectorApproval, ConsoleSession } from "../../lib/gateway-contracts/security-contracts";
import type { RuntimeMessage } from "../../lib/gateway-contracts/activity-resource-contracts";
import type { ConsoleRuntimeTarget } from "../use-gateway-resources";
import type { ConnectorActivityDialog } from "./connector-activity-dialog";
import type { ConsoleRecoveryPanel } from "./console-recovery-panel";
import type { NoLiveSession } from "./no-live-session";
import type { PtyConsole } from "./pty-console";
import type { useConsoleWorkspaceSession } from "./use-console-workspace-session";

type Completion = () => void | Promise<unknown>;
type WorkspaceSession = ReturnType<typeof useConsoleWorkspaceSession>;
type Recovery = ComponentProps<typeof ConsoleRecoveryPanel>;
type LiveSession = ComponentProps<typeof PtyConsole>["session"] &
  NonNullable<ComponentProps<typeof NoLiveSession>["lastSession"]> &
  Pick<ConsoleSession, "name"> & { id?: number };

export type ConsoleWorkspaceActions = {
  selectProfile: (_profileID: string) => void;
  openApproval: (_approval: ConnectorApproval) => void | Promise<unknown>;
  openMessages: Completion;
  refreshSessions: Completion;
  startLiveSession: Completion;
  endLiveSession: Completion;
  interruptSession: Completion;
  startStructuredSession: WorkspaceSession["startStructured"];
  endStructuredSession: WorkspaceSession["endStructured"];
  restartSession: Completion;
  selectLiveSessionName: WorkspaceSession["selectLiveSessionName"];
  startLiveSessionWithOptions: (_options?: Record<string, unknown> & { name?: string }) => void | Promise<unknown>;
  openActivity: Completion;
  refreshActivity: Completion;
  sendInput: ComponentProps<typeof PtyConsole>["onInput"];
  resizeSession: ComponentProps<typeof PtyConsole>["onResize"];
};

export type ConsoleWorkspaceSessionView = {
  selectedSession: LiveSession;
  selectedSessionLive: boolean;
  selectedStructuredSession: WorkspaceSession["selectedStructuredSession"];
  sessionsState: string;
  targetUsesLiveConsole: boolean;
};

export type ConsoleToolbarSlotProps = {
  theme: "light" | "dark";
  selectedTarget: GatewayTarget | null;
  selectedRuntimeTarget: ConsoleRuntimeTarget | null;
  selectedSession: LiveSession;
  selectedSessionLive: boolean;
  selectedUnreadMessages: RuntimeMessage[];
  liveConsoleTargets: ConsoleRuntimeTarget[];
  onOpenMessages: ConsoleWorkspaceActions["openMessages"];
  onRefreshSessions: ConsoleWorkspaceActions["refreshSessions"];
  onNewSession: ConsoleWorkspaceActions["startLiveSession"];
  onEndSession: ConsoleWorkspaceActions["endLiveSession"];
  onInterrupt: ConsoleWorkspaceActions["interruptSession"];
  structuredSession: WorkspaceSession["selectedStructuredSession"];
  onNewStructuredSession: ConsoleWorkspaceActions["startStructuredSession"];
  onEndStructuredSession: ConsoleWorkspaceActions["endStructuredSession"];
};

export type ConsoleWorkspaceSlotProps = {
  target: GatewayTarget;
  approvals: ComponentProps<typeof ConnectorActivityDialog>["approvals"];
  theme: "light" | "dark";
  session: LiveSession | WorkspaceSession["selectedStructuredSession"];
  selectedSessionLive: boolean;
  selectedRuntimeTarget: ConsoleRuntimeTarget | null;
  onNewStructuredSession: ConsoleWorkspaceActions["startStructuredSession"];
  onNewLiveSession: ConsoleWorkspaceActions["startLiveSessionWithOptions"];
  onSelectLiveSessionName: ConsoleWorkspaceActions["selectLiveSessionName"];
  onEndLiveSession: ConsoleWorkspaceActions["endLiveSession"];
  onOpenActivity: ConsoleWorkspaceActions["openActivity"];
  onRefreshActivity: ConsoleWorkspaceActions["refreshActivity"];
  children?: ReactNode;
};

export type ConsoleWorkspacePanelProps = {
  actions: ConsoleWorkspaceActions;
  approvals: ConsoleWorkspaceSlotProps["approvals"];
  connectorView: {
    Console: ComponentType<ConsoleWorkspaceSlotProps> | null;
    ToolbarActions: ComponentType<ConsoleToolbarSlotProps> | null;
  };
  liveConsoleTargets: ConsoleRuntimeTarget[];
  sessionView: ConsoleWorkspaceSessionView;
  targetView: {
    selectedRuntimeTarget: ConsoleRuntimeTarget | null;
    selectedTarget: GatewayTarget | null;
    selectedTargetProfiles: GatewayTarget[];
    selectedPendingApprovals: ConnectorApproval[];
    runningApprovalCount: number;
    selectedUnreadMessages: RuntimeMessage[];
  };
  theme: "light" | "dark";
  warnings: {
    bannerCount: number;
    showAlwaysRun: boolean;
    alwaysRunTokenCount: number;
    temporaryAlwaysRunLabels: string[];
    runningRequest: Recovery["request"] | null;
    now: number;
    restartAction: Recovery["action"];
    newSessionError: string;
  };
};
