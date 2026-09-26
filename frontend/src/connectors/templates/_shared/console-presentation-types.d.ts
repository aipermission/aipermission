import type { ConsoleRuntimeTarget } from "../../../components/use-gateway-resources";

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
export type ConsolePresentationModel = {
  targetDisplayName: (_options: ConsolePresentationOptions) => string;
  targetSubtitle: (_options: ConsoleSubtitleOptions) => string;
  targetProfileLabel: (_options: ConsolePresentationOptions) => string;
  usesLiveConsole: (_options: ConsolePresentationOptions) => boolean;
  recoverableRunningActions: (_options: ConsolePresentationOptions) => readonly string[];
};
export type NativeConsolePresentation<Target> = {
  decodeTarget: (_target: ConsolePresentationTarget) => Target;
  model: {
    targetDisplayName: (_options: { target?: Target | null }) => string;
    targetSubtitle: (_options: { target: Target }) => string;
    targetProfileLabel: (_options: { target?: Target | null }) => string;
    usesLiveConsole: (_options: { target?: Target | null }) => boolean;
    recoverableRunningActions: (_options: { target?: Target | null }) => readonly string[];
  };
  subtitle?: (_options: { target: Target; runtimeTarget?: ConsoleRuntimeTarget | null }) => string;
};
