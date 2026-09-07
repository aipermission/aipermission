import { useEffect, useEffectEvent, useMemo, useState } from "react";
import { useRequestGuard } from "../../../lib/request-guard";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { dockerConsoleSessionName } from "./container-console-panel";
import { resourceKey, resourceSearchValues } from "./helpers";
import { useDockerLifecycle } from "./use-docker-lifecycle";

const emptyResources = Object.freeze({ images: [], networks: [], volumes: [] });
const resourceListActions = Object.freeze({ images: "list_images", networks: "list_networks", volumes: "list_volumes" });

export function useDockerBrowser({
  target,
  approvals,
  session,
  selectedSessionLive,
  onNewLiveSession,
  onSelectLiveSessionName,
  onRefreshActivity,
}) {
  const [resourceView, setResourceView] = useState("containers");
  const [containers, setContainers] = useState([]);
  const [resources, setResources] = useState(emptyResources);
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

  const activeItems = useMemo(
    () => (approvals?.data || []).filter((item) => item.target_ref === target.ref),
    [approvals?.data, target.ref],
  );
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
  const filteredItems = useMemo(
    () => filterDockerResources(resourceView, activeResourceList, filter),
    [activeResourceList, filter, resourceView],
  );
  const lifecycle = useDockerLifecycle({ selectedContainer, runAction: runDockerAction, refreshContainers });
  const refreshContainersForEffect = useEffectEvent(() => refreshContainers());
  const refreshResourceForEffect = useEffectEvent((kind) => refreshResource(kind));
  const resetLifecycleForEffect = useEffectEvent(() => lifecycle.resetLifecycle());

  useEffect(() => {
    setResourceView("containers");
    setContainers([]);
    setResources(emptyResources);
    setSelectedID("");
    setSelectedResourceID("");
    setFilter("");
    setViewMode("logs");
    setResult(null);
    setResultSearch("");
    setState({ state: "idle", error: "", message: "" });
    resetLifecycleForEffect();
  }, [target.ref]);

  useEffect(() => {
    void refreshContainersForEffect();
  }, [target.ref]);

  useEffect(() => {
    if (resourceView === "containers") return;
    void refreshResourceForEffect(resourceView);
  }, [resourceView, target.ref]);

  useEffect(() => {
    if (pendingConsoleName && selectedContainerConsoleLive) setPendingConsoleName("");
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
    const item = await runDockerAction({
      actionName: resourceListActions[kind],
      input: {},
      reason: `manual Docker browser ${kind} list`,
      busy: "loading",
      showResult: false,
      channel: `list:${kind}`,
    });
    if (!item) return;
    const next = item.output?.[kind] || [];
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
    clearResult();
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
    clearResult();
  }

  function selectContainer(container) {
    if (selectedContainer && (selectedContainer.id === container.id || selectedContainer.name === container.name)) {
      setSelectedID("");
      clearResult();
      return;
    }
    setSelectedID(container.id || container.name);
    clearResult();
    if (viewMode === "inspect") {
      void inspectContainer(container);
    } else if (viewMode === "console") {
      openContainerConsole(container);
    } else {
      void readLogs(container);
    }
  }

  function switchResourceView(kind) {
    if (resourceView === kind) return;
    setResourceView(kind);
    setFilter("");
    clearResult();
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

  function clearResult() {
    setResult(null);
    setResultSearch("");
  }

  return {
    resourceView,
    filteredItems,
    visibleCount: activeResourceList.length,
    selectedContainer,
    selectedResourceID,
    selectedResource,
    selectedContainerRef,
    filter,
    setFilter,
    tail,
    setTail,
    viewMode,
    result,
    resultSearch,
    setResultSearch,
    state,
    latestAction: activeItems[0] || null,
    selectedContainerConsoleLive,
    consolePending: pendingConsoleName === expectedConsoleSessionName,
    confirmDialog: lifecycle.confirmDialog,
    closeConfirmDialog: lifecycle.closeConfirmDialog,
    refreshResource,
    switchResourceView,
    selectResource,
    readLogs,
    inspectContainer,
    openContainerConsole,
    startContainerConsole,
    openLifecycle: lifecycle.openLifecycle,
    confirmLifecycle: lifecycle.confirmLifecycle,
  };
}

export function filterDockerResources(resourceView, items, filter) {
  const query = String(filter || "")
    .trim()
    .toLowerCase();
  if (!query) return items;
  return items.filter((item) =>
    resourceSearchValues(resourceView, item).some((value) =>
      String(value || "")
        .toLowerCase()
        .includes(query),
    ),
  );
}
