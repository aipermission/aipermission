import type { ComponentType, Dispatch, SetStateAction } from "react";
import type { CredentialResource, GatewayTarget } from "../../lib/gateway-contracts/core-resource-contracts";
import type { ConnectorConnectionResult } from "../../connectors/editor/use-connector-connection-tests";
import type { ConsoleRuntimeTarget } from "../use-gateway-resources";
import type { ConsoleToolbarSlotProps, ConsoleWorkspaceSlotProps } from "./console-workspace-types";

export type ConsoleOperation = {
  open: boolean;
  connector_kind: string;
  type?: string;
  state?: string;
  error?: string | null;
  [field: string]: unknown;
};
export type ConsoleOperationResult = {
  message?: string;
  testKey?: string;
  test?: ConnectorConnectionResult;
  startConsoleSession?: boolean;
};
export type CompletedConsoleOperation = {
  connector_kind: string;
  runtimeTarget?: ConsoleRuntimeTarget | null;
};
export type ConsoleOperationSlotProps = {
  value: ConsoleOperation;
  credentials: CredentialResource[];
  onChange: Dispatch<SetStateAction<ConsoleOperation>>;
  onOperationComplete: (_result: ConsoleOperationResult, _operation: CompletedConsoleOperation) => void | Promise<void>;
};
export type ConsoleTemplateSlots = {
  Console?: ComponentType<ConsoleWorkspaceSlotProps> | null;
  ToolbarActions?: ComponentType<ConsoleToolbarSlotProps> | null;
  Operations?: ComponentType<ConsoleOperationSlotProps> | null;
};
export type ConsoleTemplateResolver = (_kind: string) => ConsoleTemplateSlots | null;
export type ConsoleConnectorViewOptions = {
  resolveTemplate?: ConsoleTemplateResolver;
  selectedTarget: Pick<GatewayTarget, "connector_kind"> | null;
};
