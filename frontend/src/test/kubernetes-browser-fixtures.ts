import { renderHook } from "@testing-library/react";
import { vi } from "vitest";
import { connectorActionRequest } from "./connector-action-fixtures";
import { apiPost } from "../lib/api.ts";
import type { KubernetesBrowserProps, useKubernetesBrowser } from "../connectors/templates/kubernetes/use-kubernetes-browser";
import type { ConnectorActionResponse } from "../lib/gateway-contracts/security-contracts";

export const pods = [
  { namespace: "default", name: "api-a", node: "worker-1", phase: "Running" },
  { namespace: "default", name: "api-b", node: "worker-2", phase: "Running" },
];

export function resetBrowserAPI() {
  vi.mocked(apiPost).mockReset();
  vi.mocked(apiPost).mockImplementation(async (_path, payload: Record<string, unknown>) =>
    completed(
      connectorActionRequest(payload).action_name,
      responseFor(connectorActionRequest(payload).action_name, connectorActionRequest(payload).input),
    ),
  );
}

export function browserRenderer(useBrowser: typeof useKubernetesBrowser) {
  return (overrides: Partial<KubernetesBrowserProps> = {}) => {
    const props: KubernetesBrowserProps = {
      target: { ref: "kubernetes:1:1" },
      approvals: { data: [] },
      session: { name: "", active: false },
      selectedSessionLive: false,
      onNewLiveSession: vi.fn(),
      onSelectLiveSessionName: vi.fn(),
      onRefreshActivity: vi.fn(),
      ...overrides,
    };
    return { ...renderHook((next) => useBrowser(next), { initialProps: props }), props };
  };
}

export function completed(actionName: string, output: unknown, targetRef = "kubernetes:1:1"): ConnectorActionResponse {
  return {
    request_id: 1,
    status: "completed",
    target_ref: targetRef,
    connector_kind: "kubernetes",
    action_name: actionName,
    retry_policy: { class: "read_only", guidance: "Safe to retry." },
    output,
  };
}

export function responseFor(actionName: string, input: Record<string, unknown>) {
  const outputs: Record<string, unknown> = {
    list_namespaces: { namespaces: [{ name: "default" }] },
    list_workloads: { workloads: [] },
    list_pods: { pods },
    get_logs: { logs: `logs for ${input.pod}` },
  };
  return outputs[actionName] || {};
}
