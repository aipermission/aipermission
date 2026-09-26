import { useOutletContext } from "react-router";
import type { useGatewayResources } from "../components/use-gateway-resources.ts";
import type { useConsoleSessionCoordinator } from "../components/console/use-console-session-coordinator.ts";
import type { Theme } from "./theme.ts";

type Resources = ReturnType<typeof useGatewayResources>;
type Coordinator = ReturnType<typeof useConsoleSessionCoordinator>;
export type GatewayContext = Pick<
  Resources,
  | "status"
  | "liveConsoleTargets"
  | "targets"
  | "credentials"
  | "tokens"
  | "connectorActionApprovals"
  | "messages"
  | "mcpRuntime"
  | "loadStatus"
  | "loadTargets"
  | "loadCredentials"
  | "loadTokens"
  | "loadConnectorActionApprovals"
  | "loadMessages"
  | "markRuntimeMessagesRead"
  | "setMCPRuntimeEnabled"
  | "gatewayState"
  | "runConnectorActionApproval"
  | "declineConnectorActionApproval"
> & {
  refreshAll: (_generation?: number) => Promise<void>;
  consoleSessions: Coordinator["sessions"];
  loadConsoleSessions: Coordinator["loadSessions"];
  ensureConsoleSession: Coordinator["ensureSession"];
  newConsoleSession: Coordinator["newSession"];
  attachConsoleSession: Coordinator["attachSession"];
  closeConsoleSession: Coordinator["closeSession"];
  cancelConsoleCommand: Coordinator["cancelCommand"];
  restartConsoleRuntime: Coordinator["restartRuntime"];
  sendConsoleInput: Coordinator["sendInput"];
  resizeConsoleSession: Coordinator["resizeSession"];
  theme: Theme;
  toggleTheme: () => void;
};

export function useGateway() {
  return useOutletContext<GatewayContext>();
}
