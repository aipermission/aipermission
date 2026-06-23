import { Container, FileJson, LoaderCircle, Play, Power, RefreshCcw, RotateCcw, Square } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Dialog } from "../../../components/ui/dialog";
import { Input } from "../../../components/ui/form";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { apiPost } from "../../../lib/api";

export function DockerConnectorConsoleTemplate({ target, approvals, theme, session, onNewStructuredSession, onRefreshActivity }) {
  const activeSession = session || { active: false, startedAt: "" };
  const [resourceView, setResourceView] = useState("containers");
  const [containers, setContainers] = useState([]);
  const [resources, setResources] = useState({ images: [], networks: [], volumes: [] });
  const [selectedID, setSelectedID] = useState("");
  const [selectedResourceID, setSelectedResourceID] = useState("");
  const [filter, setFilter] = useState("");
  const [tail, setTail] = useState(200);
  const [viewMode, setViewMode] = useState("logs");
  const [result, setResult] = useState(null);
  const [resultSearch, setResultSearch] = useState("");
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const [confirmDialog, setConfirmDialog] = useState({ open: false, title: "", description: "", details: [], actionName: "", pending: false });
  const panelClass = theme === "light" ? "bg-white text-stone-900" : "bg-[#1e1e1e] text-stone-100";
  const mutedClass = theme === "light" ? "text-stone-500" : "text-stone-400";
  const borderClass = theme === "light" ? "border-stone-200" : "border-stone-700";
  const subtlePanelClass = theme === "light" ? "bg-stone-50" : "bg-[#252526]";
  const inputClass = theme === "light" ? "border-stone-300 bg-white text-stone-900 placeholder:text-stone-400" : "border-stone-700 bg-[#1a1a1a] text-stone-100 placeholder:text-stone-500";
  const rowHoverClass = theme === "light" ? "hover:bg-stone-50" : "hover:bg-stone-800/60";
  const activeRowClass = theme === "light" ? "border-emerald-200 bg-emerald-50 text-emerald-950" : "border-emerald-700 bg-emerald-950/40 text-emerald-100";
  const activeItems = useMemo(() => (approvals?.data || []).filter((item) => item.target_ref === target.ref), [approvals?.data, target.ref]);
  const latestAction = activeItems[0] || null;
  const selectedContainer = containers.find((container) => container.id === selectedID || container.name === selectedID) || null;
  const activeResourceList = resourceView === "containers" ? containers : resources[resourceView] || [];
  const selectedResource = resourceView === "containers" ? selectedContainer : activeResourceList.find((item) => resourceKey(resourceView, item) === selectedResourceID) || null;
  const showingInspect = viewMode === "inspect";
  const filteredItems = useMemo(() => {
    const query = filter.trim().toLowerCase();
    if (!query) return activeResourceList;
    return activeResourceList.filter((item) => resourceSearchValues(resourceView, item).some((value) => String(value || "").toLowerCase().includes(query)));
  }, [activeResourceList, filter, resourceView]);

  useEffect(() => {
    setResourceView("containers");
    setContainers([]);
    setResources({ images: [], networks: [], volumes: [] });
    setSelectedID("");
    setSelectedResourceID("");
    setFilter("");
    setViewMode("logs");
    setResult(null);
    setResultSearch("");
    setState({ state: "idle", error: "", message: "" });
  }, [target.ref, activeSession.active, activeSession.startedAt]);

  useEffect(() => {
    if (!activeSession.active) return;
    void refreshContainers();
  }, [activeSession.active, activeSession.startedAt, target.ref]);

  useEffect(() => {
    if (!activeSession.active || resourceView === "containers") return;
    void refreshResource(resourceView);
  }, [activeSession.active, activeSession.startedAt, resourceView, target.ref]);

  async function runDockerAction({ actionName, input = {}, reason, busy = "running", showResult = true }) {
    setState({ state: busy, error: "", message: "" });
    try {
      const item = await apiPost("/api/connector-actions/local-run", {
        target_ref: target.ref,
        action_name: actionName,
        input,
        reason,
      });
      setState({ state: "idle", error: "", message: showResult ? item.display_text || "" : "" });
      if (showResult) setResult(item);
      await onRefreshActivity?.();
      return item;
    } catch (error) {
      setState({ state: "error", error: error.message || "Docker action failed.", message: "" });
      throw error;
    }
  }

  async function refreshContainers() {
    const item = await runDockerAction({
      actionName: "list_containers",
      input: { all: true },
      reason: "manual Docker browser container list",
      busy: "loading",
      showResult: false,
    });
    const next = item.output?.containers || [];
    setContainers(next);
    setSelectedID((current) => (current && next.some((container) => container.id === current || container.name === current) ? current : ""));
  }

  async function refreshResource(kind = resourceView) {
    if (kind === "containers") {
      await refreshContainers();
      return;
    }
    const actionName = { images: "list_images", networks: "list_networks", volumes: "list_volumes" }[kind];
    const outputKey = kind;
    const item = await runDockerAction({
      actionName,
      input: {},
      reason: `manual Docker browser ${kind} list`,
      busy: "loading",
      showResult: false,
    });
    const next = item.output?.[outputKey] || [];
    setResources((current) => ({ ...current, [kind]: next }));
    setSelectedResourceID((current) => (current && next.some((entry) => resourceKey(kind, entry) === current) ? current : ""));
  }

  async function readLogs(container = selectedContainer) {
    if (!container) return;
    setViewMode("logs");
    await runDockerAction({
      actionName: "container_logs",
      input: { container: container.name || container.id, tail: Number(tail) || 200 },
      reason: "manual Docker browser logs read",
      busy: "loading",
    });
  }

  function selectContainer(container) {
    if (selectedContainer && (selectedContainer.id === container.id || selectedContainer.name === container.name)) {
      setSelectedID("");
      setResult(null);
      setResultSearch("");
      return;
    }
    setSelectedID(container.id || container.name);
    setResult(null);
    setResultSearch("");
    if (viewMode === "inspect") {
      void inspectContainer(container);
    } else {
      void readLogs(container);
    }
  }

  function selectResource(kind, item) {
    if (kind === "containers") {
      selectContainer(item);
      return;
    }
    const key = resourceKey(kind, item);
    if (selectedResourceID === key) {
      setSelectedResourceID("");
      return;
    }
    setSelectedResourceID(key);
    setResult(null);
    setResultSearch("");
  }

  function switchResourceView(kind) {
    if (resourceView === kind) return;
    setResourceView(kind);
    setFilter("");
    setResult(null);
    setResultSearch("");
    setSelectedID("");
    setSelectedResourceID("");
  }

  async function inspectContainer(container = selectedContainer) {
    if (!container) return;
    setViewMode("inspect");
    await runDockerAction({
      actionName: "inspect_container",
      input: { container: container.name || container.id },
      reason: "manual Docker browser inspect",
      busy: "loading",
    });
  }

  function openLifecycle(actionName) {
    if (!selectedContainer) return;
    const verb = actionName.replace("_container", "");
    setConfirmDialog({
      open: true,
      title: `${capitalize(verb)} Docker container`,
      description: `This will ${verb} the selected container through the Docker connector.`,
      details: [
        { label: "Container", value: selectedContainer.name || selectedContainer.id },
        { label: "Image", value: selectedContainer.image },
        { label: "Current status", value: selectedContainer.status },
      ],
      actionName,
      pending: false,
    });
  }

  async function confirmLifecycle() {
    if (!confirmDialog.actionName || !selectedContainer) return;
    setConfirmDialog((current) => ({ ...current, pending: true }));
    const input = { container: selectedContainer.name || selectedContainer.id };
    if (confirmDialog.actionName === "stop_container" || confirmDialog.actionName === "restart_container") {
      input.timeout_seconds = 10;
    }
    try {
      await runDockerAction({
        actionName: confirmDialog.actionName,
        input,
        reason: "manual Docker browser lifecycle action",
        busy: "writing",
      });
      setConfirmDialog({ open: false, title: "", description: "", details: [], actionName: "", pending: false });
      await refreshContainers();
    } catch {
      setConfirmDialog((current) => ({ ...current, pending: false }));
    }
  }

  if (!activeSession.active) {
    return (
      <div className={`grid min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
        <div className="grid place-items-center p-8 text-center">
          <div className="grid max-w-lg gap-4">
            <Container className={`mx-auto h-10 w-10 ${mutedClass}`} />
            <div>
              <h3 className="text-lg font-semibold">No active Docker session</h3>
              <p className={`mt-2 text-sm ${mutedClass}`}>Start a structured session to browse scoped Docker containers through the connector approval, history, and audit pipeline.</p>
            </div>
            <Button type="button" className="mx-auto" onClick={onNewStructuredSession}>
              Start Docker session
            </Button>
          </div>
        </div>
        <DockerEndpointFooter target={target} borderClass={borderClass} mutedClass={mutedClass} />
      </div>
    );
  }

  return (
    <div className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
      <div className="grid min-h-0 gap-4 overflow-hidden p-4 lg:grid-cols-[360px_minmax(0,1fr)]">
        <section className={`grid min-h-0 grid-rows-[auto_auto_auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${borderClass} ${subtlePanelClass}`}>
          <div className={`border-b p-3 ${borderClass}`}>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <p className="text-sm font-semibold">{resourceLabel(resourceView)}</p>
                <p className={`text-xs ${mutedClass}`}>{activeResourceList.length} visible in this profile scope</p>
              </div>
              <div className="flex items-center gap-2">
                {latestAction ? <Badge tone={latestAction.status === "failed" ? "bad" : latestAction.status === "completed" ? "good" : "warn"}>{latestAction.action_name}</Badge> : null}
                <Button type="button" variant="outline" className="h-8 w-8 px-0" title={`Refresh ${resourceLabel(resourceView).toLowerCase()}`} onClick={() => refreshResource(resourceView)} disabled={state.state !== "idle"}>
                  <RefreshCcw className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>
          </div>
          <div className={`grid grid-cols-4 gap-1 border-b p-2 ${borderClass}`}>
            {["containers", "images", "networks", "volumes"].map((kind) => (
              <button
                type="button"
                key={kind}
                className={`rounded-md px-2 py-1.5 text-xs font-semibold ${resourceView === kind ? "bg-emerald-600 text-white" : theme === "light" ? "text-stone-600 hover:bg-stone-100" : "text-stone-300 hover:bg-stone-800"}`}
                onClick={() => switchResourceView(kind)}
              >
                {resourceTabLabel(kind)}
              </button>
            ))}
          </div>
          <div className={`border-b p-3 ${borderClass}`}>
            <Input className={inputClass} value={filter} onChange={(event) => setFilter(event.target.value)} placeholder={`Search ${resourceLabel(resourceView).toLowerCase()}`} />
          </div>
          <div className="min-h-0 overflow-auto">
            {filteredItems.length === 0 ? (
              <div className={`p-4 text-sm ${mutedClass}`}>No {resourceLabel(resourceView).toLowerCase()} matched this scope or search.</div>
            ) : (
              filteredItems.map((item) => {
                const key = resourceKey(resourceView, item);
                const active = resourceView === "containers" ? selectedContainer && (selectedContainer.id === item.id || selectedContainer.name === item.name) : selectedResourceID === key;
                return (
                  <button
                    type="button"
                    key={key}
                    className={`grid w-full gap-1 border-b px-3 py-3 text-left text-sm ${borderClass} ${rowHoverClass} ${active ? activeRowClass : ""}`}
                    onClick={() => selectResource(resourceView, item)}
                  >
                    <span className="flex min-w-0 items-center justify-between gap-3">
                      <span className="truncate font-semibold">{resourcePrimary(resourceView, item)}</span>
                      <Badge tone={resourceTone(resourceView, item)}>{resourceStatus(resourceView, item)}</Badge>
                    </span>
                    <span className={`truncate text-xs ${mutedClass}`}>{resourceSecondary(resourceView, item)}</span>
                    <span className={`truncate text-xs ${mutedClass}`}>{resourceTertiary(resourceView, item)}</span>
                  </button>
                );
              })
            )}
          </div>
        </section>

        <section className={`grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${borderClass} ${subtlePanelClass}`}>
          <div className={`border-b p-3 ${borderClass}`}>
            <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
              <div className="min-w-0">
                <div className="flex min-w-0 items-center gap-2">
                  <p className="truncate text-sm font-semibold">{selectedResource ? resourcePrimary(resourceView, selectedResource) : `Select ${resourceSingular(resourceView)}`}</p>
                  {state.state !== "idle" ? (
                    <span className={`inline-flex shrink-0 items-center gap-1 text-xs ${mutedClass}`}>
                      <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                      Loading
                    </span>
                  ) : null}
                </div>
                <p className={`truncate text-xs ${mutedClass}`}>{selectedResource ? resourceSecondary(resourceView, selectedResource) : resourcePlaceholder(resourceView)}</p>
              </div>
              {resourceView === "containers" && selectedContainer ? (
                <div className="flex flex-wrap items-center justify-end gap-2">
                  <Button type="button" variant="outline" className="h-8 px-2 text-xs" onClick={showingInspect ? () => readLogs() : () => inspectContainer()} disabled={state.state !== "idle"} title={showingInspect ? "Show container logs" : "Inspect container"}>
                    {showingInspect ? <RefreshCcw className="h-3.5 w-3.5" /> : <FileJson className="h-3.5 w-3.5" />}
                    {showingInspect ? "Logs" : "Inspect"}
                  </Button>
                  {!showingInspect ? (
                    <>
                      <label className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide">
                        Tail
                        <Input className={`h-8 w-24 ${inputClass}`} type="number" min="1" max="2000" value={tail} onChange={(event) => setTail(event.target.value)} />
                      </label>
                      <Button type="button" variant="outline" className="h-8 w-8 px-0" onClick={() => readLogs()} disabled={state.state !== "idle"} title="Refresh logs">
                        <RefreshCcw className="h-3.5 w-3.5" />
                      </Button>
                    </>
                  ) : null}
                  <Button type="button" variant="outline" className="h-8 w-8 px-0" onClick={() => openLifecycle("start_container")} disabled={state.state !== "idle"} title="Start container">
                    <Play className="h-3.5 w-3.5" />
                  </Button>
                  <Button type="button" variant="outline" className="h-8 w-8 px-0" onClick={() => openLifecycle("stop_container")} disabled={state.state !== "idle"} title="Stop container">
                    <Square className="h-3.5 w-3.5" />
                  </Button>
                  <Button type="button" variant="outline" className="h-8 w-8 px-0" onClick={() => openLifecycle("restart_container")} disabled={state.state !== "idle"} title="Restart container">
                    <RotateCcw className="h-3.5 w-3.5" />
                  </Button>
                </div>
              ) : null}
            </div>
          </div>
          {state.error ? (
            <div className={`border-b px-3 py-2 text-right text-xs text-red-500 ${borderClass}`}>
              <span className="break-words">{state.error}</span>
            </div>
          ) : null}
          <div className="grid min-h-0 overflow-hidden p-3">
            {!selectedResource ? (
              <div className={`grid place-items-center rounded-lg border border-dashed p-8 text-center text-sm ${borderClass} ${mutedClass}`}>{resourcePlaceholder(resourceView)}</div>
            ) : resourceView !== "containers" ? (
              <DockerResourceDetail resourceView={resourceView} item={selectedResource} search={resultSearch} onSearch={setResultSearch} inputClass={inputClass} />
            ) : state.state !== "idle" && !result ? (
              <div className={`grid h-full min-h-0 place-items-center rounded-lg border border-dashed p-8 text-center text-sm ${borderClass} ${mutedClass}`}>
                <span className="inline-flex items-center gap-2">
                  <LoaderCircle className="h-4 w-4 animate-spin" />
                  Loading {showingInspect ? "inspect metadata" : "logs"} for {selectedContainer.name || selectedContainer.id}...
                </span>
              </div>
            ) : result ? (
              <DockerResultView item={result} search={resultSearch} onSearch={setResultSearch} inputClass={inputClass} />
            ) : (
              <div className={`grid place-items-center rounded-lg border border-dashed p-8 text-center text-sm ${borderClass} ${mutedClass}`}>Logs will appear here after the container is loaded.</div>
            )}
          </div>
        </section>
      </div>
      <DockerEndpointFooter target={target} borderClass={borderClass} mutedClass={mutedClass} />
      <Dialog
        open={confirmDialog.open}
        title={confirmDialog.title}
        description={confirmDialog.description}
        size="md"
        onClose={() => setConfirmDialog({ open: false, title: "", description: "", details: [], actionName: "", pending: false })}
        closeDisabled={confirmDialog.pending}
      >
        <div className="grid gap-4">
          <div className="grid gap-2 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-950">
            {confirmDialog.details.map((detail) => (
              <div className="grid grid-cols-[120px_minmax(0,1fr)] gap-3" key={detail.label}>
                <span className="font-semibold">{detail.label}</span>
                <span className="min-w-0 break-words font-mono text-xs">{detail.value || "-"}</span>
              </div>
            ))}
          </div>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={() => setConfirmDialog({ open: false, title: "", description: "", details: [], actionName: "", pending: false })} disabled={confirmDialog.pending}>
              Cancel
            </Button>
            <Button type="button" onClick={confirmLifecycle} disabled={confirmDialog.pending}>
              <Power className="h-4 w-4" />
              {confirmDialog.pending ? "Running..." : "Run action"}
            </Button>
          </div>
        </div>
      </Dialog>
    </div>
  );
}

function DockerResultView({ item, search, onSearch, inputClass }) {
  const output = item.output || {};
  const isLogs = item.action_name === "container_logs" && output.logs;
  const isInspect = item.action_name === "inspect_container";
  const text = isLogs ? formatDockerLogs(output.logs) : JSON.stringify(output, null, 2);
  const copyValue = output.logs ? output.logs : JSON.stringify(output, null, 2);
  const title = dockerResultTitle(item);
  const subtitle = dockerResultSubtitle(item, output);
  if (isInspect) {
    const rawValue = JSON.stringify(output, null, 2);
    return (
      <div className="grid min-h-0 grid-rows-[auto_minmax(0,450px)_auto_minmax(0,1fr)] overflow-hidden">
        <DockerResultHeader title={title} subtitle={subtitle} />
        <DockerInspectSummary output={output} />
        <div className="mt-3 flex items-center justify-between gap-3">
          <p className="truncate text-xs font-semibold uppercase tracking-wide text-stone-500">Docker inspect raw data</p>
          <div className="flex min-w-0 items-center justify-end gap-2">
            <Input className={`h-8 w-56 text-xs ${inputClass || ""}`} value={search} onChange={(event) => onSearch?.(event.target.value)} placeholder="Search raw data" />
            <CopyButton value={rawValue} variant="outline" className="h-8 px-2 text-xs" />
          </div>
        </div>
        <div className="mt-2 grid min-h-0 overflow-hidden">
          <TerminalBlock className="min-h-0 whitespace-pre-wrap break-words text-xs [overflow-wrap:anywhere]" surface="dark">
            <HighlightedText text={rawValue} query={search} />
          </TerminalBlock>
        </div>
      </div>
    );
  }
  return (
    <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden">
      <DockerResultHeader title={title} subtitle={subtitle} copyValue={copyValue} search={search} onSearch={onSearch} inputClass={inputClass} />
      <TerminalBlock
        className={isLogs ? "h-full min-h-0 max-h-full overflow-auto whitespace-pre text-xs" : "min-h-0 whitespace-pre-wrap break-words text-xs [overflow-wrap:anywhere]"}
        surface={isLogs ? "log" : "dark"}
        style={isLogs ? { whiteSpace: "pre", overflowWrap: "normal", wordBreak: "normal" } : { whiteSpace: "pre-wrap", overflowWrap: "anywhere", wordBreak: "break-word" }}
      >
        <HighlightedText text={text} query={search} />
      </TerminalBlock>
    </div>
  );
}

function DockerResultHeader({ title, subtitle, copyValue, search, onSearch, inputClass }) {
  return (
    <div className="mb-2 flex items-center justify-between gap-3">
      <div className="min-w-0">
        <p className="truncate text-xs font-semibold uppercase tracking-wide text-stone-500">{title}</p>
        {subtitle ? <p className="truncate text-xs text-stone-500">{subtitle}</p> : null}
      </div>
      <div className="flex min-w-0 items-center justify-end gap-2">
        {onSearch ? <Input className={`h-8 w-56 text-xs ${inputClass || ""}`} value={search} onChange={(event) => onSearch(event.target.value)} placeholder="Search logs" /> : null}
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

function dockerResultTitle(item) {
  if (item.action_name === "container_logs") return "Container logs";
  if (item.action_name === "inspect_container") return "Docker inspect metadata";
  return String(item.action_name || "Docker action").replaceAll("_", " ");
}

function dockerResultSubtitle(item, output) {
  if (item.action_name === "container_logs") {
    const container = output.container || {};
    const name = container.name || container.id || "";
    const tail = output.tail ? `tail ${output.tail}` : "";
    return [name, tail].filter(Boolean).join(" · ");
  }
  if (item.action_name === "inspect_container") {
    const container = output.container || {};
    return container.name || container.id || "";
  }
  return item.display_text || "";
}

function DockerInspectSummary({ output }) {
  const inspect = Array.isArray(output.inspect) ? output.inspect[0] || {} : {};
  const container = output.container || {};
  const state = inspect.State || {};
  const config = inspect.Config || {};
  const hostConfig = inspect.HostConfig || {};
  const networkSettings = inspect.NetworkSettings || {};
  const ports = summarizePorts(networkSettings.Ports);
  const networks = summarizeNetworks(networkSettings.Networks);
  const mounts = Array.isArray(inspect.Mounts) ? inspect.Mounts : [];
  const labels = config.Labels && typeof config.Labels === "object" ? config.Labels : {};
  const health = state.Health || {};
  const rows = [
    ["Name", stripSlash(inspect.Name) || container.name],
    ["Image", config.Image || container.image || inspect.Image],
    ["State", [state.Status || container.state, state.Running === true ? "running" : "", state.Restarting === true ? "restarting" : ""].filter(Boolean).join(" / ")],
    ["Status", container.status],
    ["Created", inspect.Created],
    ["Started", state.StartedAt],
    ["Finished", state.FinishedAt],
    ["Exit code", state.ExitCode],
    ["Health", health.Status],
    ["Restart count", inspect.RestartCount],
    ["Entrypoint", arrayOrString(config.Entrypoint)],
    ["Command", arrayOrString(config.Cmd)],
    ["Working dir", config.WorkingDir],
    ["User", config.User],
    ["Network mode", hostConfig.NetworkMode],
    ["Networks", networks],
    ["Ports", ports],
    ["Mounts", mounts.map((mount) => `${mount.Type || "mount"} ${mount.Source || ""} -> ${mount.Destination || ""}`).filter(Boolean).join("\n")],
    ["Labels", Object.keys(labels).length ? `${Object.keys(labels).length} labels` : ""],
  ].filter(([, value]) => value !== undefined && value !== null && String(value).trim() !== "");

  return (
    <div className="min-h-0 overflow-auto rounded-md border border-stone-700 bg-[#1a1a1a] p-3">
      <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
        {rows.map(([label, value]) => (
          <div key={label} className="min-w-0 rounded border border-stone-700 bg-[#202020] p-2">
            <p className="text-[10px] font-semibold uppercase tracking-wide text-stone-500">{label}</p>
            <p className="mt-1 whitespace-pre-wrap break-words font-mono text-xs text-stone-100">{String(value)}</p>
          </div>
        ))}
      </div>
    </div>
  );
}

function DockerResourceDetail({ resourceView, item, search, onSearch, inputClass }) {
  const rawValue = JSON.stringify(item || {}, null, 2);
  const rows = resourceDetailRows(resourceView, item);
  return (
    <div className="grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden">
      <DockerResultHeader title={`${resourceSingular(resourceView)} metadata`} subtitle={resourceSecondary(resourceView, item)} />
      <div className="mb-3 min-h-0 overflow-auto rounded-md border border-stone-700 bg-[#1a1a1a] p-3">
        <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
          {rows.map(([label, value]) => (
            <div key={label} className="min-w-0 rounded border border-stone-700 bg-[#202020] p-2">
              <p className="text-[10px] font-semibold uppercase tracking-wide text-stone-500">{label}</p>
              <p className="mt-1 whitespace-pre-wrap break-words font-mono text-xs text-stone-100">{String(value)}</p>
            </div>
          ))}
        </div>
      </div>
      <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden">
        <div className="mb-2 flex items-center justify-between gap-3">
          <p className="truncate text-xs font-semibold uppercase tracking-wide text-stone-500">{resourceSingular(resourceView)} raw data</p>
          <div className="flex min-w-0 items-center justify-end gap-2">
            <Input className={`h-8 w-56 text-xs ${inputClass || ""}`} value={search} onChange={(event) => onSearch?.(event.target.value)} placeholder="Search raw data" />
            <CopyButton value={rawValue} variant="outline" className="h-8 px-2 text-xs" />
          </div>
        </div>
        <TerminalBlock className="min-h-0 whitespace-pre-wrap break-words text-xs [overflow-wrap:anywhere]" surface="dark">
          <HighlightedText text={rawValue} query={search} />
        </TerminalBlock>
      </div>
    </div>
  );
}

function resourceDetailRows(kind, item = {}) {
  if (kind === "images") {
    return [
      ["Repository", item.repository],
      ["Tag", item.tag],
      ["Image ID", item.id],
      ["Digest", item.digest],
      ["Size", item.size],
      ["Created", item.created_since || item.created_at],
      ["Visible containers", item.containers ?? 0],
    ].filter(([, value]) => value !== undefined && value !== null && String(value).trim() !== "");
  }
  if (kind === "networks") {
    return [
      ["Name", item.name],
      ["Network ID", item.id],
      ["Driver", item.driver],
      ["Scope", item.scope],
      ["IPv6", item.ipv6],
      ["Internal", item.internal],
      ["Visible containers", item.containers ?? 0],
      ["Labels", item.labels],
    ].filter(([, value]) => value !== undefined && value !== null && String(value).trim() !== "");
  }
  if (kind === "volumes") {
    return [
      ["Name", item.name],
      ["Driver", item.driver],
      ["Scope", item.scope],
      ["Mountpoint", item.mountpoint],
      ["Visible containers", item.containers ?? 0],
      ["Labels", item.labels],
    ].filter(([, value]) => value !== undefined && value !== null && String(value).trim() !== "");
  }
  return Object.entries(item).map(([key, value]) => [key, typeof value === "string" ? value : JSON.stringify(value)]);
}

function formatDockerLogs(logs) {
  return String(logs || "")
    .split("\n")
    .map((line) => formatDockerLogLine(line))
    .join("\n");
}

function formatDockerLogLine(line) {
  const text = String(line || "");
  if (!text.trim()) return text;
  const firstBrace = text.indexOf("{");
  const lastBrace = text.lastIndexOf("}");
  if (firstBrace < 0 || lastBrace <= firstBrace) return text;
  const prefixText = text.slice(0, firstBrace).trim();
  const jsonText = text.slice(firstBrace, lastBrace + 1);
  try {
    const payload = JSON.parse(jsonText);
    const timestamp = payload.Timestamp || prefixText || "";
    const level = payload.Level || payload.level || payload.Severity || "";
    const message = payload.MessageTemplate || payload.RenderedMessage || payload.Message || payload.message || jsonText;
    const lines = [];
    const prefix = [timestamp, level ? `[${level}]` : ""].filter(Boolean).join(" ");
    lines.push(prefix || "Docker log");
    lines.push(`  ${String(message)}`);
    if (payload.Exception) lines.push(`  Exception: ${String(payload.Exception)}`);
    const properties = payload.Properties && typeof payload.Properties === "object" ? payload.Properties : null;
    if (properties) {
      const details = Object.entries(properties)
        .slice(0, 8)
        .map(([key, value]) => `${key}=${shortValue(value)}`);
      if (details.length > 0) lines.push(`  ${details.join(" ")}`);
    }
    return lines.join("\n");
  } catch {
    return text;
  }
}

function stripSlash(value) {
  return String(value || "").replace(/^\//, "");
}

function arrayOrString(value) {
  if (Array.isArray(value)) return value.join(" ");
  return value;
}

function summarizePorts(ports) {
  if (!ports || typeof ports !== "object") return "";
  return Object.entries(ports)
    .map(([containerPort, bindings]) => {
      if (!Array.isArray(bindings) || bindings.length === 0) return containerPort;
      return bindings.map((binding) => `${binding.HostIp || "0.0.0.0"}:${binding.HostPort || ""}->${containerPort}`).join("\n");
    })
    .filter(Boolean)
    .join("\n");
}

function summarizeNetworks(networks) {
  if (!networks || typeof networks !== "object") return "";
  return Object.entries(networks)
    .map(([name, network]) => `${name}${network?.IPAddress ? ` ${network.IPAddress}` : ""}`)
    .join("\n");
}

function shortValue(value) {
  const text = typeof value === "string" ? value : JSON.stringify(value);
  if (!text) return "";
  return text.length > 80 ? `${text.slice(0, 77)}...` : text;
}

function resourceLabel(kind) {
  if (kind === "images") return "Images";
  if (kind === "networks") return "Networks";
  if (kind === "volumes") return "Volumes";
  return "Containers";
}

function resourceTabLabel(kind) {
  if (kind === "containers") return "Ctrs";
  if (kind === "networks") return "Nets";
  if (kind === "volumes") return "Vols";
  return "Images";
}

function resourceSingular(kind) {
  if (kind === "images") return "image";
  if (kind === "networks") return "network";
  if (kind === "volumes") return "volume";
  return "container";
}

function resourceKey(kind, item = {}) {
  if (kind === "images") return item.id || `${item.repository || ""}:${item.tag || ""}`;
  if (kind === "networks") return item.id || item.name;
  if (kind === "volumes") return item.name;
  return item.id || item.name;
}

function resourcePrimary(kind, item = {}) {
  if (kind === "images") return imageRef(item);
  return item.name || item.id || "-";
}

function resourceSecondary(kind, item = {}) {
  if (kind === "containers") return [item.image, item.compose_project ? `project ${item.compose_project}` : ""].filter(Boolean).join(" · ");
  if (kind === "images") return [item.id, item.size].filter(Boolean).join(" · ");
  if (kind === "networks") return [item.driver, item.scope].filter(Boolean).join(" · ");
  if (kind === "volumes") return [item.driver, item.mountpoint].filter(Boolean).join(" · ");
  return "";
}

function resourceTertiary(kind, item = {}) {
  if (kind === "containers") return [item.status, item.compose_service ? `service ${item.compose_service}` : ""].filter(Boolean).join(" · ");
  if (kind === "images") return [item.created_since || item.created_at, item.containers ? `${item.containers} containers` : ""].filter(Boolean).join(" · ");
  if (kind === "networks") return item.containers ? `${item.containers} visible containers` : item.labels || "";
  if (kind === "volumes") return item.containers ? `${item.containers} visible containers` : item.labels || "";
  return "";
}

function resourceStatus(kind, item = {}) {
  if (kind === "containers") return item.health || item.state || "unknown";
  if (kind === "images") return item.size || "image";
  if (kind === "networks") return item.driver || "network";
  if (kind === "volumes") return item.driver || "volume";
  return "item";
}

function resourceTone(kind, item = {}) {
  if (kind === "containers") {
    if (item.health === "unhealthy") return "bad";
    if (item.state === "running") return "good";
    return "neutral";
  }
  return "neutral";
}

function resourceSearchValues(kind, item = {}) {
  if (kind === "images") return [imageRef(item), item.id, item.size, item.created_since, item.created_at];
  if (kind === "networks") return [item.name, item.id, item.driver, item.scope, item.labels];
  if (kind === "volumes") return [item.name, item.driver, item.scope, item.mountpoint, item.labels];
  return [item.name, item.id, item.image, item.state, item.status, item.health, item.ports, item.compose_project, item.compose_service];
}

function resourcePlaceholder(kind) {
  if (kind === "images") return "Select an image to inspect read-only image metadata.";
  if (kind === "networks") return "Select a network to inspect read-only network metadata.";
  if (kind === "volumes") return "Select a volume to inspect read-only volume metadata.";
  return "Choose a visible container to read logs, inspect metadata, or run lifecycle actions.";
}

function imageRef(item = {}) {
  if (!item.repository) return item.id || "-";
  if (!item.tag || item.tag === "<none>") return item.repository;
  return `${item.repository}:${item.tag}`;
}

function DockerEndpointFooter({ target, borderClass, mutedClass }) {
  return (
    <div className={`flex min-h-[44px] items-center justify-between gap-3 border-t px-4 py-2 text-xs ${borderClass}`}>
      <span className={mutedClass}>Docker transport</span>
      <span className="truncate font-mono">{target.config?.transport_target_ref || "not configured"}</span>
    </div>
  );
}

function capitalize(value) {
  const text = String(value || "");
  return text ? text[0].toUpperCase() + text.slice(1) : text;
}
