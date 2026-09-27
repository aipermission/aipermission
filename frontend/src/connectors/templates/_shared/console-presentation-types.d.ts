import type { ConsoleRuntimeTarget } from "../../../components/use-gateway-resources";
import type { CoreResourceModel } from "../../../components/use-gateway-core-resources";
import type { GatewayTarget } from "../../../lib/gateway-contracts/core-resource-contracts";

export type ConsolePresentationTarget = {
  ref: string;
  connector_kind: string;
  target_name?: string;
  name?: string;
  profile_label?: string;
  config?: Record<string, unknown>;
  public?: Record<string, unknown>;
};
export type ConsolePresentationOptions = { target?: ConsolePresentationTarget | null };
export type ConsoleSubtitleOptions = { target: ConsolePresentationTarget; runtimeTarget?: ConsoleRuntimeTarget | null };
export type ConsolePresentationModel = CoreResourceModel & {
  kind: string;
  targetDisplayName: (_options: ConsolePresentationOptions) => string;
  targetSubtitle: (_options: ConsoleSubtitleOptions) => string;
  targetProfileLabel: (_options: ConsolePresentationOptions) => string;
  usesLiveConsole: (_options: ConsolePresentationOptions) => boolean;
  recoverableRunningActions: (_options: ConsolePresentationOptions) => readonly string[];
  liveConsoleRuntimeTarget?: (_options: { target: GatewayTarget }) => ConsoleRuntimeTarget;
};
export type NativeConsolePresentation<Target> = {
  kind: string;
  decodeTarget: (_target: ConsolePresentationTarget) => Target;
  model: CoreResourceModel & {
    targetDisplayName: (_options: { target?: Target | null }) => string;
    targetSubtitle: (_options: { target: Target }) => string;
    targetProfileLabel: (_options: { target?: Target | null }) => string;
    usesLiveConsole: (_options: { target?: Target | null }) => boolean;
    recoverableRunningActions: (_options: { target?: Target | null }) => readonly string[];
  };
  subtitle?: (_options: { target: Target; runtimeTarget?: ConsoleRuntimeTarget | null }) => string;
  runtimeTarget?: (_options: { target: GatewayTarget }) => ConsoleRuntimeTarget;
};
