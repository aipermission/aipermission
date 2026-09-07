import { useCallback, useState } from "react";
import { apiGet, apiPut } from "../lib/api";
import { useRequestGuard } from "../lib/request-guard";
import { normalizeCredentialResources } from "./app-shell-runtime";

const loadingList = { state: "loading", data: [], error: null };

export function useGatewayCoreResources({ connectorKinds, pollIsCurrent, resolveConnectorModel }) {
  const [status, setStatus] = useState({ state: "loading", data: null, error: null });
  const [targets, setTargets] = useState(loadingList);
  const [credentials, setCredentials] = useState({ ...loadingList, errors: [] });
  const [tokens, setTokens] = useState(loadingList);
  const [mcpRuntime, setMCPRuntime] = useState({
    state: "loading",
    data: { enabled: false, start_enabled: false },
    error: null,
  });
  const requests = useRequestGuard("gateway-core-resources");

  const loadStatus = useCallback(
    async (generation) => {
      const request = requests.begin("status");
      try {
        const data = await apiGet("/api/status", { signal: request.signal });
        if (!request.isCurrent() || !pollIsCurrent(generation)) return;
        setStatus({ state: "ready", data, error: null });
      } catch (error) {
        if (!request.isCurrent() || !pollIsCurrent(generation)) return;
        setStatus({ state: "error", data: null, error: error.message });
      } finally {
        request.complete();
      }
    },
    [pollIsCurrent, requests],
  );

  const loadTargets = useCallback(
    async (generation) => {
      const request = requests.begin("targets");
      try {
        const data = await apiGet("/api/targets", { signal: request.signal });
        if (!request.isCurrent() || !pollIsCurrent(generation)) return [];
        const items = data.items || [];
        setTargets({ state: "ready", data: items, error: null });
        return items;
      } catch (error) {
        if (!request.isCurrent() || !pollIsCurrent(generation)) return [];
        setTargets({ state: "error", data: [], error: error.message });
        return [];
      } finally {
        request.complete();
      }
    },
    [pollIsCurrent, requests],
  );

  const loadCredentials = useCallback(
    async (generation) => {
      const request = requests.begin("credentials");
      try {
        const results = await Promise.allSettled(
          connectorKinds.map(async (kind) => {
            const model = resolveConnectorModel(kind);
            if (!model?.loadCredentialResources) return [];
            return normalizeCredentialResources(kind, await model.loadCredentialResources());
          }),
        );
        if (!request.isCurrent() || !pollIsCurrent(generation)) return [];
        const data = results.flatMap((result) => (result.status === "fulfilled" ? result.value : []));
        const errors = results
          .map((result, index) =>
            result.status === "rejected" ? `${connectorKinds[index]}: ${result.reason?.message || result.reason}` : "",
          )
          .filter(Boolean);
        setCredentials({ state: "ready", data, error: null, errors });
        return data;
      } catch (error) {
        if (!request.isCurrent() || !pollIsCurrent(generation)) return [];
        setCredentials({ state: "error", data: [], error: error.message, errors: [] });
        return [];
      } finally {
        request.complete();
      }
    },
    [connectorKinds, pollIsCurrent, requests, resolveConnectorModel],
  );

  const loadTokens = useCallback(
    async (generation) => {
      const request = requests.begin("tokens");
      try {
        const data = await apiGet("/api/tokens", { signal: request.signal });
        if (!request.isCurrent() || !pollIsCurrent(generation)) return [];
        setTokens({ state: "ready", data, error: null });
        return data;
      } catch (error) {
        if (!request.isCurrent() || !pollIsCurrent(generation)) return [];
        setTokens({ state: "error", data: [], error: error.message });
        return [];
      } finally {
        request.complete();
      }
    },
    [pollIsCurrent, requests],
  );

  const loadMCPRuntime = useCallback(
    async (generation) => {
      const request = requests.begin("mcp-runtime");
      const fallback = { enabled: false, start_enabled: false };
      try {
        const data = await apiGet("/api/settings/mcp-runtime", { signal: request.signal });
        if (!request.isCurrent() || !pollIsCurrent(generation)) return fallback;
        setMCPRuntime({ state: "ready", data, error: null });
        return data;
      } catch (error) {
        if (!request.isCurrent() || !pollIsCurrent(generation)) return fallback;
        setMCPRuntime({ state: "error", data: fallback, error: error.message });
        return fallback;
      } finally {
        request.complete();
      }
    },
    [pollIsCurrent, requests],
  );

  const setMCPRuntimeEnabled = useCallback(
    async (enabled) => {
      const request = requests.begin("mcp-runtime");
      try {
        const data = await apiPut("/api/settings/mcp-runtime", { enabled });
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
