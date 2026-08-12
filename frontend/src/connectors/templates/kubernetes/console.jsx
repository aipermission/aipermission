import { RefreshCcw, RotateCcw, Search, TerminalSquare, TriangleAlert } from "lucide-react";
import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { Dialog } from "../../../components/ui/dialog";
import { Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { useRequestGuard } from "../_shared/request-guard";
import {
  resourceKey,
  resourceSearchValues,
  resourceStatus,
  resourceSubtitle,
  resourceTabs,
  resourceTertiary,
  resourceTitle,
  resourceTone,
  resourceTypeForWorkload,
} from "./helpers";
import { KubernetesPodConsolePanel, kubernetesConsoleSessionName } from "./pod-console-panel";
import { KubernetesFooter, KubernetesHeaderStatus, KubernetesResourceDetail } from "./resource-detail";

export function KubernetesConnectorConsoleTemplate({
  children,
  target,
  approvals,
  theme,
  session,
  selectedSessionLive,
  selectedRuntimeTarget,
  onNewLiveSession,
  onSelectLiveSessionName,
  onEndLiveSession,
  onRefreshActivity,
}) {
  const [tab, setTab] = useState("workloads");
  const [namespace, setNamespace] = useState("");
  const [namespaces, setNamespaces] = useState([]);
  const [filter, setFilter] = useState("");
  const [resources, setResources] = useState({});
  const [selectedKey, setSelectedKey] = useState("");
  const selectedKeyRef = useRef("");
  const [detail, setDetail] = useState(null);
  const [logs, setLogs] = useState("");
  const [resultSearch, setResultSearch] = useState("");
  const [viewMode, setViewMode] = useState("details");
  const [pendingConsoleName, setPendingConsoleName] = useState("");
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const requestGuard = useRequestGuard(target.ref);
  selectedKeyRef.current = selectedKey;
  const [confirmRestart, setConfirmRestart] = useState({ open: false, pending: false, workload: null });
  const {
    panel: panelClass,
    muted: mutedClass,
    border: borderClass,
    subtlePanel: subtlePanelClass,
    input: inputClass,
    rowHover: rowHoverClass,
    activeRow: activeRowClass,
  } = connectorConsoleTheme(theme);
  const activeItems = useMemo(
    () => (approvals?.data || []).filter((item) => item.target_ref === target.ref),
    [approvals?.data, target.ref],
  );
  const latestAction = activeItems[0] || null;
  const activeTab = resourceTabs.find((item) => item.key === tab) || resourceTabs[0];
  const activeResources = useMemo(() => resources[tab] || [], [resources, tab]);
  const selectedResource = activeResources.find((item) => resourceKey(tab, item) === selectedKey) || null;
  const expectedConsoleSessionName = selectedResource && tab === "pods" ? kubernetesConsoleSessionName(target, selectedResource) : "";
  const selectedPodConsoleLive = Boolean(selectedSessionLive && session?.name === expectedConsoleSessionName);
  const filteredResources = useMemo(() => {
    const query = filter.trim().toLowerCase();
    if (!query) return activeResources;
    return activeResources.filter((item) =>
      resourceSearchValues(tab, item).some((value) =>
        String(value || "")
          .toLowerCase()
          .includes(query),
      ),
    );
  }, [activeResources, filter, tab]);
  const refreshNamespacesForEffect = useEffectEvent(() => refreshNamespaces());
  const refreshResourceForEffect = useEffectEvent((nextTab) => refreshResource(nextTab));

  useEffect(() => {
    setNamespace("");
    setNamespaces([]);
    setResources({});
    setSelectedKey("");
    setDetail(null);
    setLogs("");
    setResultSearch("");
    setViewMode("details");
    setFilter("");
    setTab("workloads");
  }, [target.ref]);

  useEffect(() => {
    void refreshNamespacesForEffect();
    void refreshResourceForEffect("workloads");
  }, [target.ref]);

  useEffect(() => {
    if (pendingConsoleName && selectedPodConsoleLive) {
      setPendingConsoleName("");
    }
  }, [pendingConsoleName, selectedPodConsoleLive]);

  useEffect(() => {
    if (!pendingConsoleName) return undefined;
    const timeout = window.setTimeout(() => setPendingConsoleName(""), 15000);
    return () => window.clearTimeout(timeout);
  }, [pendingConsoleName]);

  async function runKubeAction({ actionName, input = {}, reason, busy = "running", channel = actionName }) {
    return runGuardedConnectorAction({
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
  }

  async function refreshNamespaces() {
    const item = await runKubeAction({
      actionName: "list_namespaces",
      reason: "manual Kubernetes browser namespace list",
      busy: "loading",
      channel: "namespaces",
    });
    if (!item) return;
    const next = Array.isArray(item.output?.namespaces) ? item.output.namespaces : [];
    setNamespaces(next);
  }

  async function refreshResource(nextTab = tab, nextNamespace = namespace) {
    const config = resourceTabs.find((item) => item.key === nextTab) || resourceTabs[0];
    const input = {};
    if (!["nodes"].includes(config.key) && nextNamespace) {
      input.namespace = nextNamespace;
    }
    if (config.key === "events") {
      input.limit = 250;
    }
    const item = await runKubeAction({
      actionName: config.action,
      input,
      reason: `manual Kubernetes browser ${config.key} list`,
      busy: "loading",
      channel: `list:${config.key}`,
    });
    if (!item) return;
    const next = Array.isArray(item.output?.[config.output]) ? item.output[config.output] : [];
    setResources((current) => ({ ...current, [config.key]: next }));
    const retainedKey = next.some((entry) => resourceKey(config.key, entry) === selectedKeyRef.current) ? selectedKeyRef.current : "";
    setSelectedKey(retainedKey);
    if (!retainedKey) {
      setDetail(null);
      setLogs("");
    }
  }

  async function selectResource(resource) {
    const key = resourceKey(tab, resource);
    const nextViewMode = tab === "pods" && viewMode === "console" ? "console" : "details";
    if (selectedKey === key) {
      setSelectedKey("");
      setDetail(null);
      setLogs("");
      setResultSearch("");
      setViewMode("details");
      return;
    }
    setSelectedKey(key);
    setLogs("");
    setResultSearch("");
    setViewMode(nextViewMode);
    if (nextViewMode === "console") {
      onSelectLiveSessionName?.(kubernetesConsoleSessionName(target, resource));
    }
    if (tab === "events") {
      setDetail({ output: { resource } });
      return;
    }
    if (tab === "nodes") {
      await describeResource({ resource_type: "node", name: resource.name });
      return;
    }
    if (tab === "workloads") {
      await describeResource({ resource_type: resourceTypeForWorkload(resource), namespace: resource.namespace, name: resource.name });
      return;
    }
    if (tab === "pods") {
      const described = await describeResource({ resource_type: "pod", namespace: resource.namespace, name: resource.name });
      if (!described) return;
      if (nextViewMode === "console") return;
      try {
        await readLogs(resource);
      } catch {
        // Keep the pod detail visible even when logs are unavailable.
      }
      return;
    }
    if (tab === "services") {
      await describeResource({ resource_type: "service", namespace: resource.namespace, name: resource.name });
      return;
    }
    if (tab === "ingress") {
      await describeResource({ resource_type: "ingress", namespace: resource.namespace, name: resource.name });
    }
  }

  async function describeResource(input) {
    const item = await runKubeAction({
      actionName: "describe_resource",
      input,
      reason: "manual Kubernetes browser resource detail",
      busy: "reading",
      channel: "detail",
    });
    if (!item) return null;
    setDetail(item);
    return item;
  }

  async function readLogs(resource = selectedResource) {
    if (!resource || tab !== "pods") return;
    const item = await runKubeAction({
      actionName: "get_logs",
      input: { namespace: resource.namespace, pod: resource.name, tail: 300 },
      reason: "manual Kubernetes browser pod logs",
      busy: "reading",
      channel: "detail",
    });
    if (!item) return;
    setLogs(item.output?.logs || item.display_text || "");
    setViewMode("details");
  }

  function openPodConsole(resource = selectedResource) {
    if (!resource || tab !== "pods") return;
    onSelectLiveSessionName?.(kubernetesConsoleSessionName(target, resource));
    setViewMode("console");
    setResultSearch("");
  }

  async function startPodConsole(resource = selectedResource) {
    if (!resource || tab !== "pods") return;
    const sessionName = kubernetesConsoleSessionName(target, resource);
    setPendingConsoleName(sessionName);
    onSelectLiveSessionName?.(sessionName);
    try {
      await onNewLiveSession?.({
        name: sessionName,
        params: { namespace: resource.namespace, pod: resource.name },
        closeExisting: false,
      });
    } catch (error) {
      setPendingConsoleName("");
      throw error;
    }
  }

  function openRestart(workload = selectedResource) {
    if (!workload || tab !== "workloads" || workload.kind !== "Deployment") return;
    setConfirmRestart({ open: true, pending: false, workload });
  }

  async function confirmRolloutRestart() {
    const workload = confirmRestart.workload;
    if (!workload) return;
    setConfirmRestart((current) => ({ ...current, pending: true }));
    try {
      const completed = await runKubeAction({
        actionName: "rollout_restart",
        input: { namespace: workload.namespace, deployment: workload.name },
        reason: "manual Kubernetes browser rollout restart",
        busy: "writing",
        channel: "restart",
      });
      if (!completed) {
        setConfirmRestart((current) => ({ ...current, pending: false }));
        return;
      }
      setConfirmRestart({ open: false, pending: false, workload: null });
      await refreshResource("workloads");
    } catch {
      setConfirmRestart((current) => ({ ...current, pending: false }));
    }
  }

  function switchTab(nextTab) {
    if (tab === nextTab) return;
    setTab(nextTab);
    setSelectedKey("");
    setDetail(null);
    setLogs("");
    setResultSearch("");
    setViewMode("details");
    setFilter("");
    void refreshResource(nextTab);
  }

  return (
    <div className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
      <div className="grid h-full min-h-0 gap-4 overflow-hidden p-4 lg:grid-cols-[380px_minmax(0,1fr)]">
        <section
          className={`grid h-full min-h-0 grid-rows-[auto_auto_auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${borderClass} ${subtlePanelClass}`}
        >
          <div className={`border-b p-3 ${borderClass}`}>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <p className="text-sm font-semibold">Kubernetes resources</p>
                <p className={`text-xs ${mutedClass}`}>
                  {filteredResources.length} shown · {activeResources.length} loaded
                </p>
              </div>
              <div className="flex items-center gap-2">
                {latestAction ? (
                  <Badge tone={latestAction.status === "failed" ? "bad" : latestAction.status === "completed" ? "good" : "warn"}>
                    {latestAction.action_name}
                  </Badge>
                ) : null}
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 w-8 px-0"
                  title="Refresh"
                  onClick={() => refreshResource(tab)}
                  disabled={state.state !== "idle"}
                >
                  <RefreshCcw className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>
          </div>
          <div className={`grid grid-cols-3 gap-1 border-b p-2 ${borderClass}`}>
            {resourceTabs.map((item) => (
              <button
                type="button"
                key={item.key}
                className={`rounded-md px-2 py-1.5 text-xs font-semibold transition ${tab === item.key ? "bg-emerald-600 text-white" : theme === "light" ? "text-stone-600 hover:bg-stone-100" : "text-stone-300 hover:bg-stone-800"}`}
                onClick={() => switchTab(item.key)}
              >
                {item.label}
              </button>
            ))}
          </div>
          <div className={`grid gap-2 border-b p-3 ${borderClass}`}>
            <Select
              value={namespace}
              onChange={(event) => {
                const nextNamespace = event.target.value;
                setNamespace(nextNamespace);
                setSelectedKey("");
                setDetail(null);
                setLogs("");
                void refreshResource(tab, nextNamespace);
              }}
            >
              <option value="">All allowed namespaces</option>
              {namespaces.map((item) => (
                <option value={item.name} key={item.name}>
                  {item.name}
                </option>
              ))}
            </Select>
            <div className="relative">
              <Search className={`pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 ${mutedClass}`} />
              <Input
                className={`pl-9 ${inputClass}`}
                value={filter}
                onChange={(event) => setFilter(event.target.value)}
                placeholder={`Filter ${activeTab.label.toLowerCase()}`}
              />
            </div>
          </div>
          <div className="min-h-0 overflow-auto">
            {filteredResources.map((resource) => (
              <button
                key={resourceKey(tab, resource)}
                type="button"
                className={`grid w-full gap-1 border-b px-3 py-3 text-left text-sm transition ${borderClass} ${rowHoverClass} ${selectedKey === resourceKey(tab, resource) ? activeRowClass : ""}`}
                onClick={() => selectResource(resource)}
              >
                <span className="flex min-w-0 items-center justify-between gap-3">
                  <span className="truncate font-semibold" title={resourceTitle(tab, resource)}>
                    {resourceTitle(tab, resource)}
                  </span>
                  {resourceStatus(tab, resource) ? <Badge tone={resourceTone(tab, resource)}>{resourceStatus(tab, resource)}</Badge> : null}
                </span>
                <span className={`truncate text-xs ${mutedClass}`}>{resourceSubtitle(tab, resource)}</span>
                <span className={`truncate text-xs ${mutedClass}`}>{resourceTertiary(tab, resource)}</span>
              </button>
            ))}
            {filteredResources.length === 0 ? (
              <Notice>{state.state === "loading" ? "Loading Kubernetes resources..." : "No resources found for this filter."}</Notice>
            ) : null}
          </div>
        </section>

        <section className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${borderClass}`}>
          <div className={`flex flex-wrap items-center justify-between gap-3 border-b p-3 ${borderClass} ${subtlePanelClass}`}>
            <div className="min-w-0">
              <p className="text-sm font-semibold">{selectedResource ? resourceTitle(tab, selectedResource) : activeTab.label}</p>
              <p className={`truncate text-xs ${mutedClass}`}>
                {selectedResource
                  ? resourceSubtitle(tab, selectedResource)
                  : "Select a resource to inspect details, logs, events, or raw JSON."}
              </p>
              <KubernetesHeaderStatus state={state} mutedClass={mutedClass} />
            </div>
            <div className="flex flex-wrap items-center gap-2">
              {selectedResource && tab === "pods" ? (
                <>
                  <Button
                    type="button"
                    variant="outline"
                    className="h-8 px-2 text-xs"
                    onClick={() => readLogs(selectedResource)}
                    disabled={state.state !== "idle"}
                  >
                    Logs
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    className="h-8 w-8 px-0"
                    onClick={() => openPodConsole(selectedResource)}
                    disabled={state.state !== "idle"}
                    title="Open live console inside this pod"
                  >
                    <TerminalSquare className="h-3.5 w-3.5" />
                  </Button>
                </>
              ) : null}
              {selectedResource && tab === "workloads" && selectedResource.kind === "Deployment" ? (
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 px-2 text-xs"
                  onClick={() => openRestart(selectedResource)}
                  disabled={state.state !== "idle"}
                >
                  <RotateCcw className="h-3.5 w-3.5" />
                  Restart
                </Button>
              ) : null}
            </div>
          </div>
          <div className="grid h-full min-h-0 grid-rows-[minmax(0,1fr)] overflow-hidden p-3">
            {tab === "pods" && viewMode === "console" ? (
              <KubernetesPodConsolePanel
                target={target}
                pod={selectedResource}
                selectedRuntimeTarget={selectedRuntimeTarget}
                session={session}
                sessionLive={selectedPodConsoleLive}
                pending={pendingConsoleName === expectedConsoleSessionName}
                theme={theme}
                mutedClass={mutedClass}
                borderClass={borderClass}
                onStart={() => startPodConsole(selectedResource)}
                onEnd={onEndLiveSession}
              >
                {children}
              </KubernetesPodConsolePanel>
            ) : (
              <KubernetesResourceDetail
                tab={tab}
                resource={selectedResource}
                detail={detail}
                logs={logs}
                search={resultSearch}
                onSearch={setResultSearch}
                inputClass={inputClass}
                mutedClass={mutedClass}
              />
            )}
          </div>
        </section>
      </div>
      <KubernetesFooter target={target} borderClass={borderClass} mutedClass={mutedClass} />
      <Dialog
        open={confirmRestart.open}
        onClose={() => setConfirmRestart({ open: false, pending: false, workload: null })}
        title="Rollout restart deployment"
        size="md"
        closeDisabled={confirmRestart.pending}
      >
        <div className="grid gap-4">
          <Notice tone="warn">
            <TriangleAlert className="mr-2 inline h-4 w-4" />
            This restarts pods for the selected deployment. Keep this action in Prompt mode unless the workflow is trusted.
          </Notice>
          <div className={`rounded-md border p-3 text-sm ${borderClass}`}>
            <p>
              <span className={mutedClass}>Namespace:</span> {confirmRestart.workload?.namespace}
            </p>
            <p>
              <span className={mutedClass}>Deployment:</span> {confirmRestart.workload?.name}
            </p>
          </div>
          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              onClick={() => setConfirmRestart({ open: false, pending: false, workload: null })}
              disabled={confirmRestart.pending}
            >
              Cancel
            </Button>
            <Button type="button" onClick={confirmRolloutRestart} disabled={confirmRestart.pending}>
              {confirmRestart.pending ? "Restarting..." : "Rollout restart"}
            </Button>
          </div>
        </div>
      </Dialog>
    </div>
  );
}
