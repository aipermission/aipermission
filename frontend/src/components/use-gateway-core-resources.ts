import { useCallback, useRef, useState } from "react";
import { apiGet, apiPut } from "../lib/api";
import { failedResource, pollReadOptions } from "../lib/async-resource";
import { useRequestGuard } from "../lib/request-guard";
import { normalizeCredentialResources } from "./app-shell-runtime";
import { credentialResourcesResponse, gatewayStatusResponse, gatewayTargetsResponse, gatewayTokensResponse, mcpRuntimeResponse } from "../lib/gateway-contracts/core-resource-contracts.ts";
import type { CredentialResource, GatewayStatus, GatewayTarget, GatewayToken, MCPRuntime } from "../lib/gateway-contracts/core-resource-contracts.ts";
import { errorMessage } from "../lib/errors.ts";

type Resource<Data> = { state: "loading" | "ready" | "error"; data: Data; error: string | null };
type Credential = ReturnType<typeof normalizeCredentialResources<CredentialResource>>[number];
export type CoreResourceModel = { loadCredentialResources?: (_options: ReturnType<typeof pollReadOptions>) => unknown | Promise<unknown> };
export type CoreResourceOptions = {
  connectorKinds: readonly string[]; pollIsCurrent: (_generation?: number) => boolean;
  resolveConnectorModel: (_kind: string) => CoreResourceModel | null | undefined;
};
function loadingList<Item>(): Resource<Item[]> { return { state: "loading", data: [], error: null }; }

export function useGatewayCoreResources({ connectorKinds, pollIsCurrent, resolveConnectorModel }: CoreResourceOptions) {
  const [status, setStatus] = useState<Resource<GatewayStatus | null>>({ state: "loading", data: null, error: null });
  const [targets, setTargets] = useState(loadingList<GatewayTarget>);
  const [credentials, setCredentials] = useState<Resource<Credential[]> & { errors: string[] }>({ ...loadingList<Credential>(), errors: [] });
  const [tokens, setTokens] = useState(loadingList<GatewayToken>);
  const [mcpRuntime, setMCPRuntime] = useState<Resource<MCPRuntime>>({
    state: "loading",
    data: { enabled: false, start_enabled: false },
    error: null,
  });
  const credentialSlices = useRef(new Map<string, Credential[]>());
  const requests = useRequestGuard("gateway-core-resources");

  const loadStatus = useCallback(
    async (generation?: number) => {
      const request = requests.begin("status");
      try {
        const data = await apiGet("/api/status", pollReadOptions(request.signal, generation));
        if (!request.isCurrent() || !pollIsCurrent(generation)) return;
        setStatus({ state: "ready", data: gatewayStatusResponse(data), error: null });
      } catch (error) {
        if (!request.isCurrent() || !pollIsCurrent(generation)) return;
        setStatus((current) => failedResource(current, error));
      } finally {
        request.complete();
      }
    },
    [pollIsCurrent, requests],
  );

  const loadTargets = useCallback(
    async (generation?: number) => {
      const request = requests.begin("targets");
      try {
        const data = await apiGet("/api/targets", pollReadOptions(request.signal, generation));
        if (!request.isCurrent() || !pollIsCurrent(generation)) return [];
        const items = gatewayTargetsResponse(data);
        setTargets({ state: "ready", data: items, error: null });
        return items;
      } catch (error) {
        if (!request.isCurrent() || !pollIsCurrent(generation)) return [];
        setTargets((current) => failedResource(current, error));
        return [];
      } finally {
        request.complete();
      }
    },
    [pollIsCurrent, requests],
  );

  const loadCredentials = useCallback(
    async (generation?: number) => {
      const request = requests.begin("credentials");
      try {
        const results = await Promise.allSettled(
          connectorKinds.map(async (kind) => {
            const model = resolveConnectorModel(kind);
            if (!model?.loadCredentialResources) return [];
            const rows = credentialResourcesResponse(await model.loadCredentialResources(pollReadOptions(request.signal, generation)));
            return normalizeCredentialResources(kind, rows);
          }),
        );
        if (!request.isCurrent() || !pollIsCurrent(generation)) return [];
        results.forEach((result, index) => {
          if (result.status === "fulfilled") credentialSlices.current.set(connectorKinds[index], result.value);
        });
        for (const kind of credentialSlices.current.keys()) {
          if (!connectorKinds.includes(kind)) credentialSlices.current.delete(kind);
        }
        const data = connectorKinds.flatMap((kind) => credentialSlices.current.get(kind) || []);
        const errors = results
          .map((result, index) =>
            result.status === "rejected" ? `${connectorKinds[index]}: ${errorMessage(result.reason)}` : "",
          )
          .filter(Boolean);
        setCredentials({ state: "ready", data, error: null, errors });
        return data;
      } catch (error) {
        if (!request.isCurrent() || !pollIsCurrent(generation)) return [];
        setCredentials((current) => failedResource(current, error, { errors: [] }));
        return [];
      } finally {
        request.complete();
      }
    },
    [connectorKinds, pollIsCurrent, requests, resolveConnectorModel],
  );

  const loadTokens = useCallback(
    async (generation?: number) => {
      const request = requests.begin("tokens");
      try {
        const response: unknown = await apiGet("/api/tokens", pollReadOptions(request.signal, generation));
        if (!request.isCurrent() || !pollIsCurrent(generation)) return [];
        const data = gatewayTokensResponse(response);
        setTokens({ state: "ready", data, error: null });
        return data;
      } catch (error) {
        if (!request.isCurrent() || !pollIsCurrent(generation)) return [];
        setTokens((current) => failedResource(current, error));
        return [];
      } finally {
        request.complete();
      }
    },
    [pollIsCurrent, requests],
  );

  const loadMCPRuntime = useCallback(
    async (generation?: number) => {
      const request = requests.begin("mcp-runtime");
      const fallback = { enabled: false, start_enabled: false };
      try {
        const response: unknown = await apiGet("/api/settings/mcp-runtime", pollReadOptions(request.signal, generation));
        if (!request.isCurrent() || !pollIsCurrent(generation)) return fallback;
        const data = mcpRuntimeResponse(response);
        setMCPRuntime({ state: "ready", data, error: null });
        return data;
      } catch (error) {
        if (!request.isCurrent() || !pollIsCurrent(generation)) return fallback;
        setMCPRuntime((current) => failedResource(current, error));
        return fallback;
      } finally {
        request.complete();
      }
    },
    [pollIsCurrent, requests],
  );

  const setMCPRuntimeEnabled = useCallback(
    async (enabled: boolean) => {
      const request = requests.begin("mcp-runtime");
      try {
        const data = mcpRuntimeResponse(await apiPut("/api/settings/mcp-runtime", { enabled }));
        if (request.isCurrent()) setMCPRuntime({ state: "ready", data, error: null });
        return data;
      } finally {
        request.complete();
      }
    },
    [requests],
  );

  return {
    credentials,
    loadCredentials,
    loadMCPRuntime,
    loadStatus,
    loadTargets,
    loadTokens,
    mcpRuntime,
    setMCPRuntimeEnabled,
    status,
    targets,
    tokens,
  };
}
