import { useEffect, useEffectEvent, useMemo, useState } from "react";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { useRequestGuard } from "../../../lib/request-guard";
import { dockerConsoleSessionName } from "./container-console-panel";
import { resourceKey, resourceSearchValues } from "./helpers";
import { DockerLifecycleDialog, emptyDockerLifecycleDialog } from "./lifecycle-dialog";
import { DockerResourceBrowser } from "./resource-browser";
import { DockerResourcePane } from "./resource-pane";

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
  const [confirmDialog, setConfirmDialog] = useState(emptyDockerLifecycleDialog);
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
      setConfirmDialog(emptyDockerLifecycleDialog());
      await refreshContainers();
    } catch {
      setConfirmDialog((current) => ({ ...current, pending: false }));
    }
  }

  return (
    <div className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
      <div className="grid h-full min-h-0 gap-4 overflow-hidden p-4 lg:grid-cols-[360px_minmax(0,1fr)]">
        <DockerResourceBrowser
          resourceView={resourceView}
          items={filteredItems}
          visibleCount={activeResourceList.length}
          selectedContainer={selectedContainer}
          selectedResourceID={selectedResourceID}
          filter={filter}
          state={state}
          latestAction={latestAction}
          theme={theme}
          classes={{
            border: borderClass,
            muted: mutedClass,
            subtlePanel: subtlePanelClass,
            input: inputClass,
            rowHover: rowHoverClass,
            activeRow: activeRowClass,
          }}
          onRefresh={() => void refreshResource(resourceView)}
          onSwitchView={switchResourceView}
          onFilter={setFilter}
          onSelect={(item) => selectResource(resourceView, item)}
        />

        <DockerResourcePane
          resourceView={resourceView}
          selectedResource={selectedResource}
          selectedContainer={selectedContainer}
          containerRef={selectedContainerRef}
          viewMode={viewMode}
          result={result}
          resultSearch={resultSearch}
          tail={tail}
          state={state}
          target={target}
          selectedRuntimeTarget={selectedRuntimeTarget}
          session={session}
          sessionLive={selectedContainerConsoleLive}
          consolePending={pendingConsoleName === expectedConsoleSessionName}
          theme={theme}
          classes={{ border: borderClass, muted: mutedClass, subtlePanel: subtlePanelClass, input: inputClass }}
          onTailChange={setTail}
          onResultSearch={setResultSearch}
          onReadLogs={() => void readLogs()}
          onInspect={() => void inspectContainer()}
          onOpenConsole={() => openContainerConsole()}
          onStartConsole={startContainerConsole}
          onEndConsole={onEndLiveSession}
          onLifecycle={openLifecycle}
        >
          {children}
        </DockerResourcePane>
      </div>
      <DockerEndpointFooter target={target} borderClass={borderClass} mutedClass={mutedClass} />
      <DockerLifecycleDialog
        dialog={confirmDialog}
        onClose={() => setConfirmDialog(emptyDockerLifecycleDialog())}
        onConfirm={confirmLifecycle}
      />
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
