import type { Dispatch, ReactNode, SetStateAction } from "react";
import type { ConsoleRuntimeTarget } from "../../../components/use-gateway-resources";
import type {
  ConsoleOperation,
  ConsoleOperationResult,
  ConsoleOperationSlotProps,
} from "../../../components/console/console-connector-view-types";

export type ConsoleRecoveryContext = {
  operation: string;
  target: Partial<ConsoleRuntimeTarget> & { id: number };
};
export type NativeRecoveryOperation = { open: boolean; state?: string; error?: string | null };
export type NativeRecoveryRenderProps<Operation> = {
  value: Operation;
  onChange: Dispatch<SetStateAction<Operation>>;
  onOperationComplete: (_result: ConsoleOperationResult) => Promise<void>;
};
export type NativeConsoleRecovery<Operation extends NativeRecoveryOperation> = {
  kind: string;
  recover: (_error: unknown, _context: ConsoleRecoveryContext) => { operation: Operation; runtimeTarget: ConsoleRuntimeTarget } | null;
  render: (_props: NativeRecoveryRenderProps<Operation>) => ReactNode;
};
export type ConsoleSessionRecovery = {
  kind: string;
  operationFromError: (_error: unknown, _context: ConsoleRecoveryContext) => ConsoleOperation | null;
  Operations: (_props: ConsoleOperationSlotProps) => ReactNode;
};
