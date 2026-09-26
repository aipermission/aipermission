import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { useRequestGuard } from "../../../lib/request-guard";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { dockerConsoleSessionName } from "./container-console-panel";
import { resourceKey, resourceSearchValues } from "./helpers";
import { useDockerLifecycle } from "./use-docker-lifecycle";
import { dockerOutputResources } from "./resource-output";
import type { DockerRunActionOptions } from "./action-types";
import type { ConnectorActionResponse, ConnectorApproval, ConsoleSession } from "../../../lib/gateway-contracts/security-contracts";
import type { DockerRuntimeTarget } from "./form-types";
import type { DockerResource, DockerResourceKind } from "./resource-types";

export interface DockerBrowserProps {
  target: Pick<DockerRuntimeTarget, "ref"> & Partial<DockerRuntimeTarget>;
  approvals?: { data?: ConnectorApproval[] };
  session?: Pick<ConsoleSession, "id" | "name"> | null;
  selectedSessionLive?: boolean;
  onNewLiveSession?: (_options: { name: string; params: { container: string }; closeExisting: boolean }) => unknown;
  onSelectLiveSessionName?: (_name: string) => unknown;
  onRefreshActivity?: () => unknown;
}

const emptyResources: Record<Exclude<DockerResourceKind, "containers">, DockerResource[]> = Object.freeze({
  images: [],
  networks: [],
  volumes: [],
});
const resourceListActions = Object.freeze({ images: "list_images", networks: "list_networks", volumes: "list_volumes" });

export function useDockerBrowser({
  target,
  approvals,
  session,
  selectedSessionLive,
  onNewLiveSession,
  onSelectLiveSessionName,
  onRefreshActivity,
}: DockerBrowserProps) {
  const [resourceView, setResourceView] = useState<DockerResourceKind>("containers");
  const [containers, setContainers] = useState<DockerResource[]>([]);
  const [resources, setResources] = useState(emptyResources);
  const [selectedID, setSelectedID] = useState("");
  const [selectedResourceID, setSelectedResourceID] = useState("");
  const [filter, setFilter] = useState("");
  const [tail, setTail] = useState<number | string>(200);
  const [viewMode, setViewMode] = useState("logs");
  const [result, setResult] = useState<ConnectorActionResponse | null>(null);
  const [resultSearch, setResultSearch] = useState("");
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const requestGuard = useRequestGuard(target.ref);

  const activeItems = useMemo(
    () => (approvals?.data || []).filter((item) => item.target_ref === target.ref),
    [approvals?.data, target.ref],
  );
  const selectedContainer = containers.find((container) => container.id === selectedID || container.name === selectedID) || null;
  const selectedContainerRef = selectedContainer ? selectedContainer.name || selectedContainer.id || "" : "";
  const expectedConsoleSessionName = selectedContainerRef ? dockerConsoleSessionName(target, selectedContainerRef) : "";
  const selectedContainerConsoleLive = Boolean(selectedSessionLive && session?.name === expectedConsoleSessionName);
  const [pendingConsoleName, setPendingConsoleName] = usePendingConsoleName(selectedContainerConsoleLive);
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
  const lifecycle = useDockerLifecycle({ targetRef: target.ref, selectedContainer, runAction: runDockerAction, refreshContainers });
  const activeResourceKind = useRef<DockerResourceKind>("containers");
  const refreshContainersForEffect = useEffectEvent(() => refreshContainers());
  const refreshResourceForEffect = useEffectEvent((kind: DockerResourceKind) => refreshResource(kind));
  const resetLifecycleForEffect = useEffectEvent(() => lifecycle.resetLifecycle());

  useEffect(() => {
    activeResourceKind.current = "containers";
    setResourceView("containers");
    setContainers([]);
    setResources(emptyResources);
    setSelectedID("");
    setSelectedResourceID("");
    setFilter("");
    setViewMode("logs");
    setResult(null);
    setResultSearch("");
    setPendingConsoleName("");
    setState({ state: "idle", error: "", message: "" });
    resetLifecycleForEffect();
  }, [target.ref, setPendingConsoleName]);

  useEffect(() => {
    void refreshContainersForEffect();
  }, [target.ref]);

  useEffect(() => {
    if (resourceView === "containers") return;
    void refreshResourceForEffect(resourceView);
  }, [resourceView, target.ref]);

  async function runDockerAction({
    actionName,
    input = {},
    reason,
    busy = "running",
    showResult = true,
    channel = actionName,
  }: DockerRunActionOptions) {
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
    await refreshResource("containers");
  }

  async function refreshResource(kind = resourceView) {
    const item = await runDockerAction({
      actionName: kind === "containers" ? "list_containers" : resourceListActions[kind],
      input: kind === "containers" ? { all: true } : {},
      reason: `manual Docker browser ${kind === "containers" ? "container" : kind} list`,
      busy: "loading",
      showResult: false,
      channel: `list:${kind}`,
    });
    if (!item) return;
    const next = dockerOutputResources(item.output, kind);
    if (kind === "containers") {
      setContainers(next);
      setSelectedID((current) =>
        current && next.some((container) => container.id === current || container.name === current) ? current : "",
      );
      return;
    }
    setResources((current) => ({ ...current, [kind]: next }));
    if (activeResourceKind.current === kind) {
      setSelectedResourceID((current) => (current && next.some((entry) => resourceKey(kind, entry) === current) ? current : ""));
    }
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
    const request = requestGuard.begin("console");
    setPendingConsoleName(expectedConsoleSessionName);
    onSelectLiveSessionName?.(expectedConsoleSessionName);
    try {
      await onNewLiveSession?.({
        name: expectedConsoleSessionName,
        params: { container: selectedContainerRef },
        closeExisting: false,
      });
    } catch (error) {
      if (!request.isCurrent()) return;
      setPendingConsoleName("");
      throw error;
    } finally {
      request.complete();
    }
  }

  function selectResource(kind: DockerResourceKind, item: DockerResource) {
    if (kind === "containers") {
      selectContainer(item);
      return;
    }
    const key = resourceKey(kind, item) || "";
    if (selectedResourceID === key) {
      setSelectedResourceID("");
      return;
    }
    setSelectedResourceID(key);
    clearResult();
  }

  function selectContainer(container: DockerResource) {
    if (selectedContainer && (selectedContainer.id === container.id || selectedContainer.name === container.name)) {
      setSelectedID("");
      clearResult();
      return;
    }
    setSelectedID(container.id || container.name || "");
    clearResult();
    if (viewMode === "inspect") {
      void inspectContainer(container);
    } else if (viewMode === "console") {
      openContainerConsole(container);
    } else {
      void readLogs(container);
    }
  }

  function switchResourceView(kind: DockerResourceKind) {
    if (resourceView === kind) return;
    activeResourceKind.current = kind;
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
    requestGuard.invalidate("detail");
    setResult(null);
    setResultSearch("");
    setState({ state: "idle", error: "", message: "" });
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
    consolePending: Boolean(pendingConsoleName && pendingConsoleName === expectedConsoleSessionName),
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

function usePendingConsoleName(live: boolean) {
  const [name, setName] = useState("");
  useEffect(() => {
    if (name && live) setName("");
  }, [name, live]);
  useEffect(() => {
    if (!name) return;
    const timeout = window.setTimeout(() => setName(""), 15000);
    return () => window.clearTimeout(timeout);
  }, [name]);
  return [name, setName] as const;
}

export function filterDockerResources(resourceView: DockerResourceKind, items: DockerResource[], filter: string) {
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
