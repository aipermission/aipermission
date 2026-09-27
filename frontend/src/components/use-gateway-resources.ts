import { useMemo } from "react";
import { supportedConnectorKinds } from "../connectors/templates/catalog";
import { getConsolePresentationModel } from "../connectors/templates/console-model-registry";
import { liveConsoleRuntimeTargets } from "./app-shell-runtime";
import { useGatewayActivityResources } from "./use-gateway-activity-resources";
import { useGatewayCoreResources } from "./use-gateway-core-resources";
import { errorMessage } from "../lib/errors";
import type { CoreResourceModel, CoreResourceOptions } from "./use-gateway-core-resources.ts";
import type { GatewayTarget } from "../lib/gateway-contracts/core-resource-contracts.ts";

export type ConsoleRuntimeTarget = {
  id: number;
  name: string;
  connector_kind?: string;
  connector_ref?: string;
  target_id?: number;
  profile_id?: number;
  target?: GatewayTarget;
  [field: string]: unknown;
};
type ResourceModel = CoreResourceModel & {
  usesLiveConsole?: (_options: { target: GatewayTarget }) => boolean;
  liveConsoleRuntimeTarget?: (_options: { target: GatewayTarget }) => ConsoleRuntimeTarget;
};
export type GatewayResourceOptions = Pick<CoreResourceOptions, "pollIsCurrent"> & {
  connectorKinds?: readonly string[];
  resolveConnectorModel?: (_kind: string) => ResourceModel | null | undefined;
};

export function useGatewayResources({
  pollIsCurrent,
  connectorKinds = supportedConnectorKinds,
  resolveConnectorModel = getConsolePresentationModel,
}: GatewayResourceOptions) {
  const core = useGatewayCoreResources({ connectorKinds, pollIsCurrent, resolveConnectorModel });
  const activity = useGatewayActivityResources({ pollIsCurrent });

  const gatewayState = useMemo(() => {
    if (core.status.state === "ready") return "running";
    if (core.status.state === "error") return "unreachable";
    return "checking";
  }, [core.status.state]);

  const liveConsoleTargets = useMemo<Pick<typeof core.targets, "state" | "error"> & { data: ConsoleRuntimeTarget[] }>(() => {
    try {
      const data = liveConsoleRuntimeTargets(core.targets.data, resolveConnectorModel);
      return { state: core.targets.state, data, error: core.targets.error };
    } catch (error) {
      return { state: "error", data: [], error: errorMessage(error, "Console runtime targets could not be projected.") };
    }
  }, [core.targets.data, core.targets.error, core.targets.state, resolveConnectorModel]);

  return { ...core, ...activity, gatewayState, liveConsoleTargets };
}
