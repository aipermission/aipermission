import { useMemo } from "react";
import { supportedConnectorKinds } from "../connectors/templates/catalog";
import { getConnectorModel } from "../connectors/templates/registry";
import { liveConsoleRuntimeTargets } from "./app-shell-runtime";
import { useGatewayActivityResources } from "./use-gateway-activity-resources";
import { useGatewayCoreResources } from "./use-gateway-core-resources";

export function useGatewayResources({
  pollIsCurrent,
  connectorKinds = supportedConnectorKinds,
  resolveConnectorModel = getConnectorModel,
}) {
  const core = useGatewayCoreResources({ connectorKinds, pollIsCurrent, resolveConnectorModel });
  const activity = useGatewayActivityResources({ pollIsCurrent });

  const gatewayState = useMemo(() => {
    if (core.status.state === "ready") return "running";
    if (core.status.state === "error") return "unreachable";
    return "checking";
  }, [core.status.state]);

  const liveConsoleTargets = useMemo(() => {
    if (core.targets.state !== "ready") return { state: core.targets.state, data: [], error: core.targets.error };
    return { state: "ready", data: liveConsoleRuntimeTargets(core.targets.data, resolveConnectorModel), error: null };
  }, [core.targets.data, core.targets.error, core.targets.state, resolveConnectorModel]);

  return { ...core, ...activity, gatewayState, liveConsoleTargets };
}
