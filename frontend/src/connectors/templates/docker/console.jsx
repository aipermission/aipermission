import { FileJson, LoaderCircle, Play, Power, RefreshCcw, RotateCcw, Square, TerminalSquare } from "lucide-react";
import { useEffect, useEffectEvent, useMemo, useState } from "react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { Dialog } from "../../../components/ui/dialog";
import { Input } from "../../../components/ui/form";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { useRequestGuard } from "../_shared/request-guard";
import { DockerContainerConsolePanel, dockerConsoleSessionName } from "./container-console-panel";
import {
  resourceKey,
  resourceLabel,
  resourcePlaceholder,
  resourcePrimary,
  resourceSearchValues,
  resourceSecondary,
  resourceSingular,
  resourceStatus,
  resourceTabLabel,
  resourceTertiary,
  resourceTone,
} from "./helpers";
import { DockerResourceDetail, DockerResultView } from "./result-view";

export function DockerConnectorConsoleTemplate({
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
  const [pendingConsoleName, setPendingConsoleName] = useState("");
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const requestGuard = useRequestGuard(target.ref);
  const [confirmDialog, setConfirmDialog] = useState({
    open: false,
    title: "",
    description: "",
    details: [],
    actionName: "",
    pending: false,
  });
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
  const selectedContainer = containers.find((container) => container.id === selectedID || container.name === selectedID) || null;
  const selectedContainerRef = selectedContainer ? selectedContainer.name || selectedContainer.id : "";
  const expectedConsoleSessionName = selectedContainerRef ? dockerConsoleSessionName(target, selectedContainerRef) : "";
  const selectedContainerConsoleLive = Boolean(selectedSessionLive && session?.name === expectedConsoleSessionName);
  const activeResourceList = useMemo(
    () => (resourceView === "containers" ? containers : resources[resourceView] || []),
    [containers, resourceView, resources],
  );
  const selectedResource =
    resourceView === "containers"
      ? selectedContainer
      : activeResourceList.find((item) => resourceKey(resourceView, item) === selectedResourceID) || null;
  const showingInspect = viewMode === "inspect";
  const filteredItems = useMemo(() => {
    const query = filter.trim().toLowerCase();
    if (!query) return activeResourceList;
    return activeResourceList.filter((item) =>
      resourceSearchValues(resourceView, item).some((value) =>
        String(value || "")
          .toLowerCase()
          .includes(query),
      ),
    );
  }, [activeResourceList, filter, resourceView]);
  const refreshContainersForEffect = useEffectEvent(() => refreshContainers());
  const refreshResourceForEffect = useEffectEvent((kind) => refreshResource(kind));

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
  }, [target.ref]);

  useEffect(() => {
    void refreshContainersForEffect();
  }, [target.ref]);

  useEffect(() => {
    if (resourceView === "containers") return;
    void refreshResourceForEffect(resourceView);
  }, [resourceView, target.ref]);

  useEffect(() => {
    if (pendingConsoleName && selectedContainerConsoleLive) {
      setPendingConsoleName("");
    }
  }, [pendingConsoleName, selectedContainerConsoleLive]);

  useEffect(() => {
    if (!pendingConsoleName) return undefined;
    const timeout = window.setTimeout(() => setPendingConsoleName(""), 15000);
    return () => window.clearTimeout(timeout);
  }, [pendingConsoleName]);

  async function runDockerAction({ actionName, input = {}, reason, busy = "running", showResult = true, channel = actionName }) {
    return runGuardedConnectorAction({
      requestGuard,
      channel,
      targetRef: target.ref,
      actionName,
      input,
      reason,
      busy,
      product: "Docker",
      setState,
      onRefreshActivity,
      successMessage: (item) => (showResult ? item.display_text || "" : ""),
      onCompleted: (item) => {
        if (showResult) setResult(item);
      },
    });
  }

  async function refreshContainers() {
    const item = await runDockerAction({
      actionName: "list_containers",
      input: { all: true },
      reason: "manual Docker browser container list",
      busy: "loading",
      showResult: false,
      channel: "list:containers",
    });
    if (!item) return;
    const next = item.output?.containers || [];
    setContainers(next);
    setSelectedID((current) =>
      current && next.some((container) => container.id === current || container.name === current) ? current : "",
    );
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
      channel: `list:${kind}`,
    });
    if (!item) return;
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
      channel: "detail",
    });
  }

  function openContainerConsole(container = selectedContainer) {
    if (!container) return;
    const containerRef = container.name || container.id;
    if (containerRef) onSelectLiveSessionName?.(dockerConsoleSessionName(target, containerRef));
    setViewMode("console");
    setResult(null);
    setResultSearch("");
  }

  async function startContainerConsole() {
    if (!selectedContainerRef) return;
    setPendingConsoleName(expectedConsoleSessionName);
    onSelectLiveSessionName?.(expectedConsoleSessionName);
    try {
      await onNewLiveSession?.({
        name: expectedConsoleSessionName,
        params: { container: selectedContainerRef },
        closeExisting: false,
      });
    } catch (error) {
      setPendingConsoleName("");
      throw error;
    }
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
    } else if (viewMode === "console") {
      openContainerConsole(container);
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
      channel: "detail",
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
      const completed = await runDockerAction({
        actionName: confirmDialog.actionName,
        input,
        reason: "manual Docker browser lifecycle action",
        busy: "writing",
        channel: "lifecycle",
      });
      if (!completed) {
        setConfirmDialog((current) => ({ ...current, pending: false }));
        return;
      }
      setConfirmDialog({ open: false, title: "", description: "", details: [], actionName: "", pending: false });
      await refreshContainers();
    } catch {
      setConfirmDialog((current) => ({ ...current, pending: false }));
    }
  }

  return (
    <div className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
      <div className="grid h-full min-h-0 gap-4 overflow-hidden p-4 lg:grid-cols-[360px_minmax(0,1fr)]">
        <section
          className={`grid h-full min-h-0 grid-rows-[auto_auto_auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${borderClass} ${subtlePanelClass}`}
        >
          <div className={`border-b p-3 ${borderClass}`}>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <p className="text-sm font-semibold">{resourceLabel(resourceView)}</p>
                <p className={`text-xs ${mutedClass}`}>{activeResourceList.length} visible in this profile scope</p>
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
                  title={`Refresh ${resourceLabel(resourceView).toLowerCase()}`}
                  onClick={() => refreshResource(resourceView)}
                  disabled={state.state !== "idle"}
                >
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
            <Input
              className={inputClass}
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
              placeholder={`Search ${resourceLabel(resourceView).toLowerCase()}`}
            />
          </div>
          <div className="min-h-0 overflow-auto">
            {filteredItems.length === 0 ? (
              <div className={`p-4 text-sm ${mutedClass}`}>
                No {resourceLabel(resourceView).toLowerCase()} matched this scope or search.
              </div>
            ) : (
              filteredItems.map((item) => {
                const key = resourceKey(resourceView, item);
                const active =
                  resourceView === "containers"
                    ? selectedContainer && (selectedContainer.id === item.id || selectedContainer.name === item.name)
                    : selectedResourceID === key;
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

        <section
          className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${borderClass} ${subtlePanelClass}`}
        >
          <div>
            <div className={`border-b p-3 ${borderClass}`}>
              <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex min-w-0 items-center gap-2">
                    <p className="truncate text-sm font-semibold">
                      {selectedResource ? resourcePrimary(resourceView, selectedResource) : `Select ${resourceSingular(resourceView)}`}
                    </p>
                    {state.state !== "idle" ? (
                      <span className={`inline-flex shrink-0 items-center gap-1 text-xs ${mutedClass}`}>
                        <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                        Loading
                      </span>
                    ) : null}
                  </div>
                  <p className={`truncate text-xs ${mutedClass}`}>
                    {selectedResource ? resourceSecondary(resourceView, selectedResource) : resourcePlaceholder(resourceView)}
                  </p>
                </div>
                {resourceView === "containers" && selectedContainer ? (
                  <div className="flex flex-wrap items-center justify-end gap-2">
                    <Button
                      type="button"
                      variant="outline"
                      className="h-8 px-2 text-xs"
                      onClick={showingInspect ? () => readLogs() : () => inspectContainer()}
                      disabled={state.state !== "idle"}
                      title={showingInspect ? "Show container logs" : "Inspect container"}
                    >
                      {showingInspect ? <RefreshCcw className="h-3.5 w-3.5" /> : <FileJson className="h-3.5 w-3.5" />}
                      {showingInspect ? "Logs" : "Inspect"}
                    </Button>
                    {!showingInspect && viewMode !== "console" ? (
                      <>
                        <label className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide">
                          Tail
                          <Input
                            className={`h-8 w-24 ${inputClass}`}
                            type="number"
                            min="1"
                            max="2000"
                            value={tail}
                            onChange={(event) => setTail(event.target.value)}
                          />
                        </label>
                        <Button
                          type="button"
                          variant="outline"
                          className="h-8 w-8 px-0"
                          onClick={() => readLogs()}
                          disabled={state.state !== "idle"}
                          title="Refresh logs"
                        >
                          <RefreshCcw className="h-3.5 w-3.5" />
                        </Button>
                      </>
                    ) : null}
                    <Button
                      type="button"
                      variant="outline"
                      className="h-8 w-8 px-0"
                      onClick={() => openContainerConsole()}
                      disabled={state.state !== "idle"}
                      title="Open live console inside this container"
                    >
                      <TerminalSquare className="h-3.5 w-3.5" />
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      className="h-8 w-8 px-0"
                      onClick={() => openLifecycle("start_container")}
                      disabled={state.state !== "idle"}
                      title="Start container"
                    >
                      <Play className="h-3.5 w-3.5" />
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      className="h-8 w-8 px-0"
                      onClick={() => openLifecycle("stop_container")}
                      disabled={state.state !== "idle"}
                      title="Stop container"
                    >
                      <Square className="h-3.5 w-3.5" />
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      className="h-8 w-8 px-0"
                      onClick={() => openLifecycle("restart_container")}
                      disabled={state.state !== "idle"}
                      title="Restart container"
                    >
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
          </div>
          <div className="grid h-full min-h-0 grid-rows-[minmax(0,1fr)] overflow-hidden p-3">
            {!selectedResource ? (
              <div
                className={`grid place-items-center rounded-lg border border-dashed p-8 text-center text-sm ${borderClass} ${mutedClass}`}
              >
                {resourcePlaceholder(resourceView)}
              </div>
            ) : resourceView !== "containers" ? (
              <DockerResourceDetail
                resourceView={resourceView}
                item={selectedResource}
                search={resultSearch}
                onSearch={setResultSearch}
                inputClass={inputClass}
              />
            ) : viewMode === "console" ? (
              <DockerContainerConsolePanel
                target={target}
                container={selectedContainer}
                containerRef={selectedContainerRef}
                selectedRuntimeTarget={selectedRuntimeTarget}
                session={session}
                sessionLive={selectedContainerConsoleLive}
                pending={pendingConsoleName === expectedConsoleSessionName}
                theme={theme}
                mutedClass={mutedClass}
                borderClass={borderClass}
                onStart={startContainerConsole}
                onEnd={onEndLiveSession}
              >
                {children}
              </DockerContainerConsolePanel>
            ) : state.state !== "idle" && !result ? (
              <div
                className={`grid h-full min-h-0 place-items-center rounded-lg border border-dashed p-8 text-center text-sm ${borderClass} ${mutedClass}`}
              >
                <span className="inline-flex items-center gap-2">
                  <LoaderCircle className="h-4 w-4 animate-spin" />
                  Loading {showingInspect ? "inspect metadata" : "logs"} for {selectedContainer.name || selectedContainer.id}...
                </span>
              </div>
            ) : result ? (
              <DockerResultView item={result} search={resultSearch} onSearch={setResultSearch} inputClass={inputClass} />
            ) : (
              <div
                className={`grid place-items-center rounded-lg border border-dashed p-8 text-center text-sm ${borderClass} ${mutedClass}`}
              >
                Logs will appear here after the container is loaded.
              </div>
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
            <Button
              type="button"
              variant="outline"
              onClick={() => setConfirmDialog({ open: false, title: "", description: "", details: [], actionName: "", pending: false })}
              disabled={confirmDialog.pending}
            >
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
