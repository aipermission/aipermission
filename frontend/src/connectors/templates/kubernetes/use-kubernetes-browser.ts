import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { useRequestGuard } from "../../../lib/request-guard";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import type { GuardedConnectorActionOptions } from "../_shared/action-runner";
import type { ConnectorActionResponse, ConnectorApproval } from "../../../lib/gateway-contracts/security-contracts";
import type { KubernetesRuntimeTarget } from "./form-types";
import type { KubernetesResource, KubernetesResourceKind } from "./resource-types";
import { kubernetesOutputField, kubernetesOutputNamespaces, kubernetesOutputResources } from "./resource-output";
import { resourceKey, resourceSearchValues, resourceTabs, resourceTypeForWorkload } from "./helpers";
import { kubernetesConsoleSessionName } from "./pod-console-panel";

export interface KubernetesBrowserProps {
  target: Pick<KubernetesRuntimeTarget, "ref"> & Partial<KubernetesRuntimeTarget>;
  approvals?: { data?: ConnectorApproval[] };
  session?: { name?: string; active?: boolean } | null;
  selectedSessionLive?: boolean;
  onNewLiveSession?: (_options: { name: string; params: { namespace?: string; pod?: string }; closeExisting: boolean }) => unknown;
  onSelectLiveSessionName?: (_name: string) => unknown;
  onRefreshActivity?: () => unknown;
}

export type KubernetesRunActionOptions = Pick<GuardedConnectorActionOptions, "actionName" | "input" | "reason" | "busy" | "channel">;

export function useKubernetesBrowser(props: KubernetesBrowserProps) {
  const { target, approvals, session, selectedSessionLive, onNewLiveSession, onSelectLiveSessionName, onRefreshActivity } = props;
  const [tab, setTab] = useState<KubernetesResourceKind>("workloads");
  const [namespace, setNamespace] = useState("");
  const [namespaces, setNamespaces] = useState<{ name: string }[]>([]);
  const [filter, setFilter] = useState("");
  const [resources, setResources] = useState<Partial<Record<KubernetesResourceKind, KubernetesResource[]>>>({});
  const [selectedKey, setSelectedKey] = useState("");
  const selectedKeyRef = useRef("");
  const [detail, setDetail] = useState<Pick<ConnectorActionResponse, "output"> | null>(null);
  const [logs, setLogs] = useState("");
  const [resultSearch, setResultSearch] = useState("");
  const [viewMode, setViewMode] = useState("details");
  const [pendingConsoleName, setPendingConsoleName] = useState("");
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const requestGuard = useRequestGuard(target.ref);
  selectedKeyRef.current = selectedKey;
  const activeTab = resourceTabs.find((item) => item.key === tab) || resourceTabs[0];
  const activeResources = useMemo(() => resources[tab] || [], [resources, tab]);
  const selectedResource = activeResources.find((item) => resourceKey(tab, item) === selectedKey) || null;
  const expectedConsoleName = selectedResource && tab === "pods" ? kubernetesConsoleSessionName(target, selectedResource) : "";
  const selectedPodConsoleLive = Boolean(selectedSessionLive && session?.name === expectedConsoleName);
  const filteredResources = useMemo(() => filterResources(tab, activeResources, filter), [activeResources, filter, tab]);
  const latestAction = useMemo(
    () => (approvals?.data || []).find((item) => item.target_ref === target.ref) || null,
    [approvals?.data, target.ref],
  );
  const refreshNamespacesForEffect = useEffectEvent(() => refreshNamespaces());
  const refreshResourceForEffect = useEffectEvent((nextTab: KubernetesResourceKind, nextNamespace: string) => refreshResource(nextTab, nextNamespace));

  useEffect(() => {
    setTab("workloads");
    setNamespace("");
    setNamespaces([]);
    setFilter("");
    setResources({});
    setSelectedKey("");
    setDetail(null);
    setLogs("");
    setResultSearch("");
    setViewMode("details");
    setPendingConsoleName("");
    setState({ state: "idle", error: "", message: "" });
  }, [target.ref]);

  useEffect(() => {
    void refreshNamespacesForEffect();
    void refreshResourceForEffect("workloads", "");
  }, [target.ref]);

  useEffect(() => {
    if (pendingConsoleName && selectedPodConsoleLive) setPendingConsoleName("");
  }, [pendingConsoleName, selectedPodConsoleLive]);

  useEffect(() => {
    if (!pendingConsoleName) return undefined;
    const timeout = window.setTimeout(() => setPendingConsoleName(""), 15000);
    return () => window.clearTimeout(timeout);
  }, [pendingConsoleName]);

  async function runAction({ actionName, input = {}, reason, busy = "running", channel = actionName }: KubernetesRunActionOptions) {
    try {
      return await runGuardedConnectorAction({
        requestGuard,
        channel,
        targetRef: target.ref,
        actionName,
        input,
        reason,
        busy,
        product: "Kubernetes",
        setState,
        onRefreshActivity,
      });
    } catch {
      return null;
    }
  }

  async function refreshNamespaces() {
    const item = await runAction({
      actionName: "list_namespaces",
      reason: "manual Kubernetes browser namespace list",
      busy: "loading",
      channel: "namespaces",
    });
    if (item) setNamespaces(kubernetesOutputNamespaces(item.output));
  }

  async function refreshResource(nextTab = tab, nextNamespace = namespace) {
    const config = resourceTabs.find((item) => item.key === nextTab) || resourceTabs[0];
    const input: Record<string, unknown> = {};
    if (config.key !== "nodes" && nextNamespace) input.namespace = nextNamespace;
    if (config.key === "events") input.limit = 250;
    const item = await runAction({
      actionName: config.action,
      input,
      reason: `manual Kubernetes browser ${config.key} list`,
      busy: "loading",
      channel: "resource-list",
    });
    if (!item) return;
    const next = kubernetesOutputResources(item.output, config.output);
    setResources((current) => ({ ...current, [config.key]: next }));
    const retainedKey = next.some((entry) => resourceKey(config.key, entry) === selectedKeyRef.current) ? selectedKeyRef.current : "";
    setSelectedKey(retainedKey);
    if (!retainedKey) clearDetail();
  }

  async function selectResource(resource: KubernetesResource) {
    const key = resourceKey(tab, resource);
    const nextMode = tab === "pods" && viewMode === "console" ? "console" : "details";
    if (selectedKey === key) {
      clearSelection();
      setViewMode("details");
      return;
    }
    setSelectedKey(key);
    clearDetail();
    setViewMode(nextMode);
    if (nextMode === "console") onSelectLiveSessionName?.(kubernetesConsoleSessionName(target, resource));
    if (tab === "events") setDetail({ output: { resource } });
    else if (tab === "nodes") await describeResource({ resource_type: "node", name: resource.name });
    else if (tab === "workloads")
      await describeResource({ resource_type: resourceTypeForWorkload(resource), namespace: resource.namespace, name: resource.name });
    else if (tab === "services" || tab === "ingress")
      await describeResource({
        resource_type: tab === "services" ? "service" : "ingress",
        namespace: resource.namespace,
        name: resource.name,
      });
    else if (tab === "pods") {
      const described = await describeResource({ resource_type: "pod", namespace: resource.namespace, name: resource.name });
      if (described && nextMode !== "console") await readLogs(resource);
    }
  }

  async function describeResource(input: Record<string, unknown>) {
    const item = await runAction({
      actionName: "describe_resource",
      input,
      reason: "manual Kubernetes browser resource detail",
      busy: "reading",
      channel: "detail",
    });
    if (item) setDetail(item);
    return item;
  }

  async function readLogs(resource = selectedResource) {
    if (!resource || tab !== "pods") return;
    const item = await runAction({
      actionName: "get_logs",
      input: { namespace: resource.namespace, pod: resource.name, tail: 300 },
      reason: "manual Kubernetes browser pod logs",
      busy: "reading",
      channel: "detail",
    });
    if (!item) return;
    const outputLogs = kubernetesOutputField(item.output, "logs");
    setLogs((typeof outputLogs === "string" && outputLogs) || item.display_text || "");
    setViewMode("details");
  }

  function openPodConsole(resource = selectedResource) {
    if (!resource || tab !== "pods") return;
    requestGuard.invalidate("detail");
    setState({ state: "idle", error: "", message: "" });
    onSelectLiveSessionName?.(kubernetesConsoleSessionName(target, resource));
    setViewMode("console");
    setResultSearch("");
  }

  async function startPodConsole(resource = selectedResource) {
    if (!resource || tab !== "pods") return;
    const name = kubernetesConsoleSessionName(target, resource);
    const request = requestGuard.begin("console");
    setPendingConsoleName(name);
    onSelectLiveSessionName?.(name);
    try {
      await onNewLiveSession?.({ name, params: { namespace: resource.namespace, pod: resource.name }, closeExisting: false });
    } catch (error) {
      if (!request.isCurrent()) return;
      setPendingConsoleName("");
      throw error;
    } finally {
      request.complete();
    }
  }

  function switchTab(nextTab: KubernetesResourceKind) {
    if (tab === nextTab) return;
    setTab(nextTab);
    clearSelection();
    setViewMode("details");
    setFilter("");
    void refreshResource(nextTab);
  }

  function changeNamespace(value: string) {
    setNamespace(value);
    clearSelection();
    void refreshResource(tab, value);
  }

  function clearDetail() {
    requestGuard.invalidate("detail");
    setDetail(null);
    setLogs("");
    setResultSearch("");
  }

  function clearSelection() {
    setSelectedKey("");
    clearDetail();
  }

  return {
    tab,
    activeTab,
    namespace,
    namespaces,
    filter,
    setFilter,
    activeResources,
    filteredResources,
    selectedKey,
    selectedResource,
    detail,
    logs,
    resultSearch,
    setResultSearch,
    viewMode,
    state,
    latestAction,
    selectedPodConsoleLive,
    consolePending: pendingConsoleName === expectedConsoleName,
    runAction,
    refreshResource,
    selectResource,
    readLogs,
    openPodConsole,
    startPodConsole,
    switchTab,
    changeNamespace,
  };
}

function filterResources(tab: KubernetesResourceKind, resources: KubernetesResource[], filter: string) {
  const query = filter.trim().toLowerCase();
  if (!query) return resources;
  return resources.filter((item) =>
    resourceSearchValues(tab, item).some((value) =>
      String(value || "")
        .toLowerCase()
        .includes(query),
    ),
  );
}
