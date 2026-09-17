import { useCallback, useRef, useState } from "react";
import { apiGet, apiPut } from "../lib/api";
import { failedResource } from "../lib/async-resource";
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
  const credentialSlices = useRef(new Map());
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
        setStatus((current) => failedResource(current, error));
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
        setTargets((current) => failedResource(current, error));
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
        results.forEach((result, index) => {
          if (result.status === "fulfilled") credentialSlices.current.set(connectorKinds[index], result.value);
        });
        for (const kind of credentialSlices.current.keys()) {
          if (!connectorKinds.includes(kind)) credentialSlices.current.delete(kind);
        }
        const data = connectorKinds.flatMap((kind) => credentialSlices.current.get(kind) || []);
        const errors = results
          .map((result, index) =>
            result.status === "rejected" ? `${connectorKinds[index]}: ${result.reason?.message || result.reason}` : "",
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
    async (generation) => {
      const request = requests.begin("tokens");
      try {
        const data = await apiGet("/api/tokens", { signal: request.signal });
        if (!request.isCurrent() || !pollIsCurrent(generation)) return [];
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
        setMCPRuntime((current) => failedResource(current, error));
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
