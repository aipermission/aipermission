import { FileJson, LoaderCircle, RefreshCcw, RotateCcw, Search, TerminalSquare, TriangleAlert, XCircle } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Dialog } from "../../../components/ui/dialog";
import { Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { apiPost } from "../../../lib/api";

const resourceTabs = Object.freeze([
  { key: "workloads", label: "Workloads", action: "list_workloads", output: "workloads" },
  { key: "pods", label: "Pods", action: "list_pods", output: "pods" },
  { key: "services", label: "Services", action: "list_services", output: "services" },
  { key: "ingress", label: "Ingress", action: "list_ingress", output: "ingress" },
  { key: "nodes", label: "Nodes", action: "list_nodes", output: "nodes" },
  { key: "events", label: "Events", action: "list_events", output: "events" },
]);

export function KubernetesConnectorConsoleTemplate({ children, target, approvals, theme, session, selectedSessionLive, selectedRuntimeTarget, onNewLiveSession, onSelectLiveSessionName, onEndLiveSession, onRefreshActivity }) {
  const [tab, setTab] = useState("workloads");
  const [namespace, setNamespace] = useState("");
  const [namespaces, setNamespaces] = useState([]);
  const [filter, setFilter] = useState("");
  const [resources, setResources] = useState({});
  const [selectedKey, setSelectedKey] = useState("");
  const [detail, setDetail] = useState(null);
  const [logs, setLogs] = useState("");
  const [resultSearch, setResultSearch] = useState("");
  const [viewMode, setViewMode] = useState("details");
  const [pendingConsoleName, setPendingConsoleName] = useState("");
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const [confirmRestart, setConfirmRestart] = useState({ open: false, pending: false, workload: null });
  const panelClass = theme === "light" ? "bg-white text-stone-900" : "bg-[#1e1e1e] text-stone-100";
  const mutedClass = theme === "light" ? "text-stone-500" : "text-stone-400";
  const borderClass = theme === "light" ? "border-stone-200" : "border-stone-700";
  const subtlePanelClass = theme === "light" ? "bg-stone-50" : "bg-[#252526]";
  const inputClass = theme === "light" ? "border-stone-300 bg-white text-stone-900 placeholder:text-stone-400" : "border-stone-700 bg-[#1a1a1a] text-stone-100 placeholder:text-stone-500";
  const rowHoverClass = theme === "light" ? "hover:bg-stone-50" : "hover:bg-stone-800/60";
  const activeRowClass = theme === "light" ? "border-emerald-200 bg-emerald-50 text-emerald-950" : "border-emerald-700 bg-emerald-950/40 text-emerald-100";
  const activeItems = useMemo(() => (approvals?.data || []).filter((item) => item.target_ref === target.ref), [approvals?.data, target.ref]);
  const latestAction = activeItems[0] || null;
  const activeTab = resourceTabs.find((item) => item.key === tab) || resourceTabs[0];
  const activeResources = resources[tab] || [];
  const selectedResource = activeResources.find((item) => resourceKey(tab, item) === selectedKey) || null;
  const expectedConsoleSessionName = selectedResource && tab === "pods" ? kubernetesConsoleSessionName(target, selectedResource) : "";
  const selectedPodConsoleLive = Boolean(selectedSessionLive && session?.name === expectedConsoleSessionName);
  const filteredResources = useMemo(() => {
    const query = filter.trim().toLowerCase();
    if (!query) return activeResources;
    return activeResources.filter((item) => resourceSearchValues(tab, item).some((value) => String(value || "").toLowerCase().includes(query)));
  }, [activeResources, filter, tab]);

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
    void refreshNamespaces();
    void refreshResource("workloads");
  }, [target.ref]);

  useEffect(() => {
    if (pendingConsoleName && selectedPodConsoleLive) {
      setPendingConsoleName("");
    }
  }, [pendingConsoleName, selectedPodConsoleLive]);

  async function runKubeAction({ actionName, input = {}, reason, busy = "running" }) {
    setState({ state: busy, error: "", message: "" });
    try {
      const item = await apiPost("/api/connector-actions/local-run", {
        target_ref: target.ref,
        action_name: actionName,
        input,
        reason,
      });
      setState({ state: "idle", error: "", message: item.display_text || "" });
      await onRefreshActivity?.();
      return item;
    } catch (error) {
      setState({ state: "error", error: error.message || "Kubernetes action failed.", message: "" });
      throw error;
    }
  }

  async function refreshNamespaces() {
    const item = await runKubeAction({ actionName: "list_namespaces", reason: "manual Kubernetes browser namespace list", busy: "loading" });
    const next = Array.isArray(item.output?.namespaces) ? item.output.namespaces : [];
    setNamespaces(next);
  }

  async function refreshResource(nextTab = tab) {
    const config = resourceTabs.find((item) => item.key === nextTab) || resourceTabs[0];
    const input = {};
    if (!["nodes"].includes(config.key) && namespace) {
      input.namespace = namespace;
    }
    if (config.key === "events") {
      input.limit = 250;
    }
    const item = await runKubeAction({
      actionName: config.action,
      input,
      reason: `manual Kubernetes browser ${config.key} list`,
      busy: "loading",
    });
    const next = Array.isArray(item.output?.[config.output]) ? item.output[config.output] : [];
    setResources((current) => ({ ...current, [config.key]: next }));
    setSelectedKey((current) => (current && next.some((entry) => resourceKey(config.key, entry) === current) ? current : ""));
    if (!next.some((entry) => resourceKey(config.key, entry) === selectedKey)) {
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
      await describeResource({ resource_type: "pod", namespace: resource.namespace, name: resource.name });
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
    const item = await runKubeAction({ actionName: "describe_resource", input, reason: "manual Kubernetes browser resource detail", busy: "reading" });
    setDetail(item);
  }

  async function readLogs(resource = selectedResource) {
    if (!resource || tab !== "pods") return;
    const item = await runKubeAction({
      actionName: "get_logs",
      input: { namespace: resource.namespace, pod: resource.name, tail: 300 },
      reason: "manual Kubernetes browser pod logs",
      busy: "reading",
    });
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
      await runKubeAction({
        actionName: "rollout_restart",
        input: { namespace: workload.namespace, deployment: workload.name },
        reason: "manual Kubernetes browser rollout restart",
        busy: "writing",
      });
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
        <section className={`grid h-full min-h-0 grid-rows-[auto_auto_auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${borderClass} ${subtlePanelClass}`}>
          <div className={`border-b p-3 ${borderClass}`}>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <p className="text-sm font-semibold">Kubernetes resources</p>
                <p className={`text-xs ${mutedClass}`}>{filteredResources.length} shown · {activeResources.length} loaded</p>
              </div>
              <div className="flex items-center gap-2">
                {latestAction ? <Badge tone={latestAction.status === "failed" ? "bad" : latestAction.status === "completed" ? "good" : "warn"}>{latestAction.action_name}</Badge> : null}
                <Button type="button" variant="outline" className="h-8 w-8 px-0" title="Refresh" onClick={() => refreshResource(tab)} disabled={state.state !== "idle"}>
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
            <Select value={namespace} onChange={(event) => { setNamespace(event.target.value); setSelectedKey(""); setDetail(null); setLogs(""); }}>
              <option value="">All allowed namespaces</option>
              {namespaces.map((item) => (
                <option value={item.name} key={item.name}>
                  {item.name}
                </option>
              ))}
            </Select>
            <div className="relative">
              <Search className={`pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 ${mutedClass}`} />
              <Input className={`pl-9 ${inputClass}`} value={filter} onChange={(event) => setFilter(event.target.value)} placeholder={`Filter ${activeTab.label.toLowerCase()}`} />
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
                  <span className="truncate font-semibold" title={resourceTitle(tab, resource)}>{resourceTitle(tab, resource)}</span>
                  {resourceStatus(tab, resource) ? <Badge tone={resourceTone(tab, resource)}>{resourceStatus(tab, resource)}</Badge> : null}
                </span>
                <span className={`truncate text-xs ${mutedClass}`}>{resourceSubtitle(tab, resource)}</span>
                <span className={`truncate text-xs ${mutedClass}`}>{resourceTertiary(tab, resource)}</span>
              </button>
            ))}
            {filteredResources.length === 0 ? <Notice>{state.state === "loading" ? "Loading Kubernetes resources..." : "No resources found for this filter."}</Notice> : null}
          </div>
        </section>

        <section className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${borderClass}`}>
          <div className={`flex flex-wrap items-center justify-between gap-3 border-b p-3 ${borderClass} ${subtlePanelClass}`}>
            <div className="min-w-0">
              <p className="text-sm font-semibold">{selectedResource ? resourceTitle(tab, selectedResource) : activeTab.label}</p>
              <p className={`truncate text-xs ${mutedClass}`}>{selectedResource ? resourceSubtitle(tab, selectedResource) : "Select a resource to inspect details, logs, events, or raw JSON."}</p>
              <KubernetesHeaderStatus state={state} mutedClass={mutedClass} />
            </div>
            <div className="flex flex-wrap items-center gap-2">
              {selectedResource && tab === "pods" ? (
                <>
                  <Button type="button" variant="outline" className="h-8 px-2 text-xs" onClick={() => readLogs(selectedResource)} disabled={state.state !== "idle"}>
                    Logs
                  </Button>
                  <Button type="button" variant="outline" className="h-8 w-8 px-0" onClick={() => openPodConsole(selectedResource)} disabled={state.state !== "idle"} title="Open live console inside this pod">
                    <TerminalSquare className="h-3.5 w-3.5" />
                  </Button>
                </>
              ) : null}
              {selectedResource && tab === "workloads" && selectedResource.kind === "Deployment" ? (
                <Button type="button" variant="outline" className="h-8 px-2 text-xs" onClick={() => openRestart(selectedResource)} disabled={state.state !== "idle"}>
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
                pending={pendingConsoleName === expectedConsoleSessionName || (state.state !== "idle" && !selectedPodConsoleLive)}
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
      <Dialog open={confirmRestart.open} onClose={() => setConfirmRestart({ open: false, pending: false, workload: null })} title="Rollout restart deployment" maxWidth="max-w-lg">
        <div className="grid gap-4">
          <Notice tone="warn">
            <TriangleAlert className="mr-2 inline h-4 w-4" />
            This restarts pods for the selected deployment. Keep this action in Prompt mode unless the workflow is trusted.
          </Notice>
          <div className={`rounded-md border p-3 text-sm ${borderClass}`}>
            <p><span className={mutedClass}>Namespace:</span> {confirmRestart.workload?.namespace}</p>
            <p><span className={mutedClass}>Deployment:</span> {confirmRestart.workload?.name}</p>
          </div>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={() => setConfirmRestart({ open: false, pending: false, workload: null })} disabled={confirmRestart.pending}>Cancel</Button>
            <Button type="button" onClick={confirmRolloutRestart} disabled={confirmRestart.pending}>
              {confirmRestart.pending ? "Restarting..." : "Rollout restart"}
            </Button>
          </div>
        </div>
      </Dialog>
    </div>
  );
}

function KubernetesResourceDetail({ tab, resource, detail, logs, search, onSearch, inputClass, mutedClass }) {
  if (!resource) {
    return (
      <div className={`grid h-full min-h-0 place-items-center rounded-lg border border-dashed p-8 text-center text-sm ${mutedClass}`}>
        Select a Kubernetes resource to inspect metadata, logs, and raw JSON.
      </div>
    );
  }
  const rawResource = detail?.output?.resource || detail?.output || resource;
  const rawValue = JSON.stringify(rawResource || {}, null, 2);
  const topTitle = kubernetesTopTitle(tab);
  const topSubtitle = kubernetesTopSubtitle(tab, resource);
  const topCopyValue = tab === "pods" ? logs : kubernetesMetadataText(tab, resource);
  const showLogSurface = tab === "pods";
  return (
    <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,600px)_auto_minmax(0,1fr)] overflow-hidden">
      <KubernetesResultHeader title={topTitle} subtitle={topSubtitle} copyValue={topCopyValue} search={search} onSearch={onSearch} inputClass={inputClass} searchPlaceholder={tab === "pods" ? "Search logs" : "Search metadata"} />
      {showLogSurface ? (
        <TerminalBlock
          className="h-full min-h-0 max-h-full overflow-auto whitespace-pre text-xs"
          surface="log"
          style={{ whiteSpace: "pre", overflowWrap: "normal", wordBreak: "normal" }}
        >
          <HighlightedText text={logs || "Click Logs to load bounded pod logs for this pod."} query={search} />
        </TerminalBlock>
      ) : (
        <div className="min-h-0 overflow-auto rounded-md border border-stone-700 bg-[#1a1a1a] p-3">
          <KubernetesSummaryCards tab={tab} resource={resource} mutedClass={mutedClass} />
          {tab === "nodes" ? (
            <p className="mt-3 rounded-md border border-amber-700/50 bg-amber-950/30 p-3 text-xs text-amber-100">
              Kubernetes nodes do not expose pod-style logs through kubectl logs. Use metadata, conditions, and events for node investigation.
            </p>
          ) : null}
          {tab === "events" && resource.message ? (
            <TerminalBlock className="mt-3 min-h-32 text-xs" surface="dark">
              <HighlightedText text={resource.message} query={search} />
            </TerminalBlock>
          ) : null}
        </div>
      )}
      <div className="mt-3 flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <FileJson className="h-3.5 w-3.5 text-stone-500" />
          <p className="truncate text-xs font-semibold uppercase tracking-wide text-stone-500">Kubernetes raw data</p>
        </div>
        <div className="flex min-w-0 items-center justify-end gap-2">
          <Input className={`h-8 w-56 text-xs ${inputClass || ""}`} value={search} onChange={(event) => onSearch?.(event.target.value)} placeholder="Search raw data" />
          <CopyButton value={rawValue} variant="outline" className="h-8 px-2 text-xs" />
        </div>
      </div>
      <div className="mt-2 grid h-full min-h-0 overflow-hidden">
        <TerminalBlock className="h-full min-h-0 whitespace-pre-wrap break-words text-xs [overflow-wrap:anywhere]" surface="dark">
          <HighlightedText text={rawValue} query={search} />
        </TerminalBlock>
      </div>
    </div>
  );
}

function KubernetesPodConsolePanel({ children, target, pod, selectedRuntimeTarget, session, sessionLive, pending, theme, mutedClass, borderClass, onStart, onEnd }) {
  const light = theme === "light";
  if (!pod) {
    return (
      <div className={`grid h-full min-h-0 place-items-center rounded-lg border border-dashed p-8 text-center text-sm ${borderClass} ${mutedClass}`}>
        Select a pod, then open a live console inside it.
      </div>
    );
  }
  const podRef = `${pod.namespace}/${pod.name}`;
  if (sessionLive) {
    return (
      <div className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${borderClass}`}>
        <div className={`flex min-w-0 items-center justify-between gap-3 border-b px-3 py-2 ${borderClass}`}>
          <div className="min-w-0">
            <p className="truncate text-xs font-semibold uppercase tracking-wide text-stone-500">Pod console</p>
            <p className="truncate font-mono text-xs">{podRef}</p>
          </div>
          <Button type="button" variant="outline" className="h-8 px-2 text-xs" onClick={onEnd} title="Close this pod console session">
            <XCircle className="h-3.5 w-3.5" />
            End
          </Button>
        </div>
        <div className="h-full min-h-0 overflow-hidden">{children}</div>
      </div>
    );
  }
  const expectedName = kubernetesConsoleSessionName(target, pod);
  if (pending) {
    return (
      <div className={`grid h-full min-h-0 place-items-center rounded-lg border border-dashed p-8 text-center ${borderClass}`}>
        <div className="grid max-w-md gap-3">
          <LoaderCircle className="mx-auto h-6 w-6 animate-spin text-emerald-500" />
          <h3 className={`text-base font-semibold ${light ? "text-stone-950" : "text-white"}`}>Connecting pod console</h3>
          <p className={`text-sm leading-6 ${mutedClass}`}>Opening an interactive shell inside <span className="font-mono">{podRef}</span>.</p>
        </div>
      </div>
    );
  }
  return (
    <div className={`grid h-full min-h-0 place-items-center rounded-lg border border-dashed p-8 text-center ${borderClass}`}>
      <div className="grid max-w-md gap-4">
        <div className={`mx-auto flex h-12 w-12 items-center justify-center rounded-full border ${light ? "border-stone-200 bg-stone-100" : "border-stone-600 bg-stone-800"}`}>
          <TerminalSquare className={`h-6 w-6 ${light ? "text-stone-600" : "text-stone-300"}`} />
        </div>
        <div className="grid gap-2">
          <h3 className={`text-base font-semibold ${light ? "text-stone-950" : "text-white"}`}>No active pod console</h3>
          <p className={`text-sm leading-6 ${mutedClass}`}>
            Start an interactive shell inside <span className="font-mono">{podRef}</span>. It uses the same live terminal as SSH and Docker consoles.
          </p>
          {!selectedRuntimeTarget ? <p className="text-xs text-red-500">This Kubernetes profile does not have a live runtime surface yet. Save the connector once, then retry.</p> : null}
        </div>
        <Button type="button" className="mx-auto" onClick={onStart} disabled={!selectedRuntimeTarget}>
          <RefreshCcw className="h-4 w-4" />
          Start Pod Console
        </Button>
      </div>
    </div>
  );
}

function kubernetesConsoleSessionName(target, pod) {
  return `kubernetes:${target?.ref || "target"}:${pod?.namespace || "namespace"}:${pod?.name || "pod"}`;
}

function KubernetesHeaderStatus({ state, mutedClass }) {
  if (state.state !== "idle" && state.state !== "error") {
    return (
      <p className="mt-1 flex min-h-4 items-center gap-1 truncate text-[11px] text-amber-500">
        <LoaderCircle className="h-3 w-3 shrink-0 animate-spin" />
        <span className="truncate">{state.state}</span>
      </p>
    );
  }
  if (state.state === "error" && state.error) {
    return <p className="mt-1 min-h-4 truncate text-[11px] text-red-500">{state.error}</p>;
  }
  return <p className={`mt-1 min-h-4 text-[11px] ${mutedClass}`}>&nbsp;</p>;
}

function KubernetesResultHeader({ title, subtitle, copyValue, search, onSearch, inputClass, searchPlaceholder }) {
  return (
    <div className="mb-2 flex items-center justify-between gap-3">
      <div className="min-w-0">
        <p className="truncate text-xs font-semibold uppercase tracking-wide text-stone-500">{title}</p>
        {subtitle ? <p className="truncate text-xs text-stone-500">{subtitle}</p> : null}
      </div>
      <div className="flex min-w-0 items-center justify-end gap-2">
        <Input className={`h-8 w-56 text-xs ${inputClass || ""}`} value={search} onChange={(event) => onSearch?.(event.target.value)} placeholder={searchPlaceholder || "Search"} />
        {copyValue ? <CopyButton value={copyValue} variant="outline" className="h-8 px-2 text-xs" /> : null}
      </div>
    </div>
  );
}

function HighlightedText({ text, query }) {
  const value = String(text || "");
  const needle = String(query || "");
  if (!needle.trim()) return value;
  const lowerValue = value.toLowerCase();
  const lowerNeedle = needle.toLowerCase();
  const parts = [];
  let index = 0;
  let matchIndex = lowerValue.indexOf(lowerNeedle, index);
  let key = 0;
  while (matchIndex !== -1) {
    if (matchIndex > index) parts.push(value.slice(index, matchIndex));
    parts.push(
      <mark key={`m-${key++}`} className="rounded bg-yellow-300 px-0.5 text-stone-950">
        {value.slice(matchIndex, matchIndex + needle.length)}
      </mark>
    );
    index = matchIndex + needle.length;
    matchIndex = lowerValue.indexOf(lowerNeedle, index);
  }
  if (index < value.length) parts.push(value.slice(index));
  return parts;
}

function kubernetesTopTitle(tab) {
  if (tab === "pods") return "Pod logs";
  if (tab === "nodes") return "Node metadata";
  if (tab === "events") return "Event details";
  return "Resource metadata";
}

function kubernetesTopSubtitle(tab, resource) {
  if (!resource) return "";
  if (tab === "pods") return `${resource.namespace}/${resource.name}`;
  if (tab === "nodes") return "Node logs are not available through kubectl logs.";
  if (tab === "events") return `${resource.namespace || "-"} · ${resource.object || "-"}`;
  return resourceSubtitle(tab, resource);
}

function kubernetesMetadataText(tab, resource) {
  return summaryRows(tab, resource)
    .map((row) => `${row.label}: ${row.value || "-"}`)
    .join("\n");
}

function KubernetesSummaryCards({ tab, resource, detail, mutedClass }) {
  if (!resource) {
    return <p className={`text-sm ${mutedClass}`}>No resource selected.</p>;
  }
  const rows = summaryRows(tab, resource, detail);
  return (
    <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
      {rows.map((row) => (
        <div key={row.label} className="rounded-md border border-stone-700/40 p-2">
          <p className={`text-[11px] uppercase ${mutedClass}`}>{row.label}</p>
          <p className="truncate text-sm font-semibold" title={String(row.value || "-")}>{row.value || "-"}</p>
        </div>
      ))}
    </div>
  );
}

function KubernetesFooter({ target, borderClass, mutedClass }) {
  return (
    <div className={`flex min-h-9 items-center justify-between border-t px-4 py-2 text-xs ${borderClass} ${mutedClass}`}>
      <span>Kubernetes transport</span>
      <span className="font-mono">{target.config?.transport_target_ref || "no transport"}</span>
    </div>
  );
}

function summaryRows(tab, resource) {
  if (tab === "workloads") return [
    { label: "Kind", value: resource.kind },
    { label: "Namespace", value: resource.namespace },
    { label: "Ready", value: resource.ready },
    { label: "Image", value: resource.image },
  ];
  if (tab === "pods") return [
    { label: "Namespace", value: resource.namespace },
    { label: "Phase", value: resource.phase },
    { label: "Ready", value: resource.ready },
    { label: "Restarts", value: resource.restarts },
  ];
  if (tab === "services") return [
    { label: "Namespace", value: resource.namespace },
    { label: "Type", value: resource.type },
    { label: "Cluster IP", value: resource.cluster_ip },
    { label: "Ports", value: resource.ports },
  ];
  if (tab === "ingress") return [
    { label: "Namespace", value: resource.namespace },
    { label: "Class", value: resource.class },
    { label: "Hosts", value: resource.hosts },
    { label: "Age", value: resource.age },
  ];
  if (tab === "nodes") return [
    { label: "Name", value: resource.name },
    { label: "Ready", value: resource.ready },
    { label: "Roles", value: resource.roles },
    { label: "Version", value: resource.version },
  ];
  return [
    { label: "Type", value: resource.type },
    { label: "Reason", value: resource.reason },
    { label: "Object", value: resource.object },
    { label: "Count", value: resource.count },
  ];
}

function resourceKey(tab, item) {
  if (!item) return "";
  if (tab === "nodes") return item.name || "";
  return `${item.namespace || ""}/${item.kind || tab}/${item.name || item.reason || item.message || ""}`;
}

function resourceTitle(tab, item) {
  if (!item) return "";
  if (tab === "events") return `${item.type || "Event"} ${item.reason || ""}`.trim();
  if (tab === "workloads") return `${item.namespace}/${item.kind}/${item.name}`;
  if (tab === "pods") return item.node || item.name;
  if (tab === "nodes") return item.name;
  return `${item.namespace}/${item.name}`;
}

function resourceSubtitle(tab, item) {
  if (!item) return "";
  if (tab === "workloads") return `ready ${item.ready || "-"} · image ${item.image || "-"}`;
  if (tab === "pods") return `${item.namespace}/${item.name}`;
  if (tab === "services") return `${item.type || "-"} · ${item.cluster_ip || "-"} · ${item.ports || "no ports"}`;
  if (tab === "ingress") return `${item.hosts || "no hosts"} · ${item.class || "no class"}`;
  if (tab === "nodes") return `ready ${item.ready || "-"} · ${item.roles || "-"} · ${item.version || "-"}`;
  return `${item.namespace || "-"} · ${item.object || "-"} · ${item.message || ""}`;
}

function resourceTertiary(tab, item) {
  if (!item) return "";
  if (tab === "pods") return `ready ${item.ready || "-"} · restarts ${item.restarts || 0} · ${item.phase || "-"}`;
  if (tab === "workloads") return `${item.namespace || "-"} · age ${item.age || "-"}`;
  if (tab === "services") return `age ${item.age || "-"} · external ${item.external_ip || "-"}`;
  if (tab === "ingress") return `namespace ${item.namespace || "-"} · age ${item.age || "-"}`;
  if (tab === "nodes") return `age ${item.age || "-"} · status ${item.ready || "-"}`;
  return item.message || "";
}

function resourceStatus(tab, item) {
  if (!item) return "";
  if (tab === "pods") return item.phase || "";
  if (tab === "workloads") return item.ready || "";
  if (tab === "services") return item.type || "";
  if (tab === "nodes") return item.ready || "";
  if (tab === "events") return item.type || "";
  return "";
}

function resourceTone(tab, item) {
  const value = String(resourceStatus(tab, item)).toLowerCase();
  if (tab === "events" && value === "warning") return "warn";
  if (value.includes("running") || value.includes("ready") || value.match(/^\d+\/\d+$/)) return "good";
  if (value.includes("pending") || value.includes("unknown")) return "warn";
  if (value.includes("failed") || value.includes("error") || value.includes("crash")) return "bad";
  return "neutral";
}

function resourceSearchValues(tab, item) {
  return [resourceTitle(tab, item), resourceSubtitle(tab, item), resourceTertiary(tab, item), item.namespace, item.name, item.kind, item.reason, item.message, item.hosts, item.image, item.node];
}

function resourceTypeForWorkload(resource) {
  const kind = String(resource?.kind || "").toLowerCase();
  if (kind === "statefulset") return "statefulset";
  if (kind === "daemonset") return "daemonset";
  return "deployment";
}
