import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { apiDelete, apiGet, apiPost } from "../lib/api";
import { connectorKindLabel } from "../connectors/templates/common";
import {
  currentHistoryPage,
  firstHistoryPage,
  nextHistoryPage,
  previousHistoryPage,
  resolvedHistoryTotal,
} from "../lib/history-pagination";
import { useRequestGuard } from "../lib/request-guard";
import type { Dispatch, SetStateAction } from "react";
import { errorMessage } from "../lib/errors";
import type { HistoryPageState } from "../lib/history-pagination";
import {
  historyEntryResponse,
  historyLabelsResponse,
  historyPageResponse,
  historyTargetsResponse,
  type HistoryEntry,
  type HistoryLabel,
  type HistoryTarget,
} from "../lib/gateway-contracts/history-resource-contract";
import { projectListResponse, type ProjectSummary } from "../lib/gateway-contracts/project-list-contract";
type Filters = typeof initialFilters;
type LoadOptions = { poll?: boolean; silent?: boolean; includeTotal?: boolean };
type Resource<T> = { state: string; data: T[]; error: string | null };
export type HistoryResource = Resource<HistoryEntry> & HistoryPageState & { total: number; nextCursor: string | null };

const initialFilters = { query: "", projectID: "", connectorKind: "", status: "", source: "", targetRef: "", labelID: "" };

export function useHistoryPageState() {
  const [filters, setFilters] = useState(initialFilters);
  const [state, setState] = useState<HistoryResource>({
    state: "idle",
    data: [],
    total: 0,
    ...firstHistoryPage(50),
    nextCursor: null,
    error: null,
  });
  const [selected, setSelected] = useState<HistoryEntry | null>(null);
  const detailOwnerRef = useRef<{ id: number; labelsChanged: boolean } | null>(null);
  const references = useHistoryReferences();
  const targetItems = useMemo(() => references.targets.data || [], [references.targets.data]);
  const targetSignature = targetItems.map((target) => `${target.ref}:${target.project_id || ""}:${target.project_name || ""}`).join(",");
  const requestScope = JSON.stringify([
    filters.query,
    filters.projectID,
    filters.connectorKind,
    filters.status,
    filters.source,
    filters.targetRef,
    filters.labelID,
    state.limit,
    targetSignature,
  ]);
  const requestGuard = useRequestGuard(requestScope);
  const filterGenerationRef = useRef(0);
  const filterTransitionPendingRef = useRef(true);
  const interactiveRequestPendingRef = useRef(false);
  const loadHistoryForEffect = useEffectEvent((page: HistoryPageState, options: LoadOptions) => loadHistory(page, options));

  const connectorKindOptions = useMemo(() => {
    const kinds = Array.from(new Set(targetItems.map((target) => target.connector_kind).filter(Boolean))).sort();
    return [{ value: "", label: "All connectors" }, ...kinds.map((kind) => ({ value: kind, label: connectorKindLabel(kind) }))];
  }, [targetItems]);

  useEffect(() => {
    const generation = ++filterGenerationRef.current;
    filterTransitionPendingRef.current = true;
    requestGuard.invalidate("list");
    requestGuard.invalidate("poll");
    setState((current) => ({ ...current, ...firstHistoryPage(current.limit), nextCursor: null }));
    const timer = window.setTimeout(() => {
      void loadHistoryForEffect(firstHistoryPage(state.limit), { includeTotal: true }).finally(() => {
        if (filterGenerationRef.current === generation) filterTransitionPendingRef.current = false;
      });
    }, 250);
    return () => window.clearTimeout(timer);
  }, [
    filters.query,
    filters.projectID,
    filters.connectorKind,
    filters.status,
    filters.source,
    filters.targetRef,
    filters.labelID,
    state.limit,
    targetSignature,
    requestGuard,
  ]);

  const hasActiveHistory = state.data.some((item) => ["pending", "pending_approval", "running", "paused"].includes(item.status));
  useEffect(() => {
    if (!hasActiveHistory) return undefined;
    let canceled = false;
    let timer: number | null = null;
    const poll = async () => {
      if (!filterTransitionPendingRef.current && !interactiveRequestPendingRef.current) {
        await loadHistoryForEffect(
          { limit: state.limit, cursor: state.cursor, pageIndex: state.pageIndex, cursorStack: state.cursorStack },
          { silent: true, includeTotal: false, poll: true },
        );
      }
      if (!canceled) timer = window.setTimeout(poll, 1500);
    };
    timer = window.setTimeout(poll, 1500);
    return () => {
      canceled = true;
      if (timer !== null) window.clearTimeout(timer);
    };
  }, [hasActiveHistory, state.cursor, state.cursorStack, state.limit, state.pageIndex]);

  const stats = useMemo(
    () => ({
      total: state.total,
      shown: state.data.length,
      active: state.data.filter((item) => ["pending", "pending_approval", "running", "paused"].includes(item.status)).length,
      failed: state.data.filter((item) => ["failed", "error", "stale", "outcome_unknown"].includes(item.status)).length,
    }),
    [state.data, state.total],
  );

  async function loadHistory(page = currentHistoryPage(state), options: LoadOptions = {}) {
    const channel = options.poll ? "poll" : "list";
    if (!options.poll) {
      interactiveRequestPendingRef.current = true;
      requestGuard.invalidate("poll");
    }
    const request = requestGuard.begin(channel);
    if (!options.silent) setState((current) => ({ ...current, state: "loading", ...page, error: null }));
    const params = buildHistoryParams({ page, options, filters, targetItems });
    try {
      const data = await apiGet(`/api/history?${params}`, { signal: request.signal });
      if (!request.isCurrent()) return;
      const verified = historyPageResponse(data);
      setState((current) => ({
        state: "ready",
        data: verified.items,
        total: resolvedHistoryTotal(current.total, verified, page),
        limit: verified.limit,
        cursor: page.cursor,
        pageIndex: page.pageIndex,
        cursorStack: page.cursorStack,
        nextCursor: verified.next_cursor || null,
        error: null,
      }));
    } catch (error) {
      if (request.isCurrent()) {
        setState((current) => ({ ...current, state: "error", data: [], total: 0, nextCursor: null, error: errorMessage(error) }));
      }
    } finally {
      if (!options.poll && request.isCurrent()) interactiveRequestPendingRef.current = false;
      request.complete();
    }
  }

  function updateFilters(updater: SetStateAction<Filters>) {
    filterGenerationRef.current += 1;
    filterTransitionPendingRef.current = true;
    requestGuard.invalidate("list");
    requestGuard.invalidate("poll");
    setState((current) => ({ ...current, ...firstHistoryPage(current.limit), nextCursor: null }));
    setFilters(updater);
  }

  async function openHistoryItem(item: HistoryEntry) {
    const request = requestGuard.begin("detail");
    const owner = { id: item.id, labelsChanged: false };
    detailOwnerRef.current = owner;
    setSelected(item);
    try {
      const detail = await apiGet(`/api/history/${item.id}`, { signal: request.signal });
      if (!request.isCurrent() || detailOwnerRef.current !== owner) return;
      const verified = historyEntryResponse(detail, item.id);
      setSelected((current) =>
        current?.id === item.id ? { ...verified, labels: owner.labelsChanged ? current.labels : verified.labels } : current,
      );
    } catch {
      // The list preview is already visible; a failed read must not undo newer labels.
    } finally {
      request.complete();
    }
  }

  function closeHistoryItem() {
    requestGuard.invalidate("detail");
    detailOwnerRef.current = null;
    setSelected(null);
  }

  function updateItemLabels(id: number, nextLabels: HistoryLabel[]) {
    const owner = detailOwnerRef.current;
    if (owner?.id === id) owner.labelsChanged = true;
    setSelected((current) => (current?.id === id ? { ...current, labels: nextLabels } : current));
    setState((current) => ({ ...current, data: current.data.map((item) => (item.id === id ? { ...item, labels: nextLabels } : item)) }));
  }

  async function attachLabel(id: number, payload: { name: string }) {
    updateItemLabels(id, historyLabelsResponse(await apiPost(`/api/history/${id}/labels`, payload)));
    await references.loadLabels();
  }

  async function detachLabel(id: number, labelID: number) {
    updateItemLabels(id, historyLabelsResponse(await apiDelete(`/api/history/${id}/labels/${labelID}`)));
    if (!filters.labelID || String(labelID) !== String(filters.labelID)) return;
    setState((current) => ({ ...current, data: current.data.filter((item) => item.id !== id), total: Math.max(0, current.total - 1) }));
  }

  return {
    filters,
    updateFilters,
    state,
    stats,
    selected,
    targetItems,
    connectorKindOptions,
    ...references,
    openHistoryItem,
    closeHistoryItem,
    attachLabel,
    detachLabel,
    refresh: () => {
      void references.loadTargets();
      void loadHistory(currentHistoryPage(state), { includeTotal: true });
    },
    previous: () => {
      const page = previousHistoryPage(state);
      if (page) void loadHistory(page);
    },
    next: () => {
      const page = nextHistoryPage(state);
      if (page) void loadHistory(page);
    },
  };
}

function buildHistoryParams({
  page,
  options,
  filters,
  targetItems,
}: {
  page: HistoryPageState;
  options: LoadOptions;
  filters: Filters;
  targetItems: HistoryTarget[];
}) {
  const params = new URLSearchParams({ limit: String(page.limit) });
  setParam(params, "cursor", page.cursor);
  if (options.includeTotal !== undefined) params.set("include_total", String(options.includeTotal));
  setParam(params, "q", filters.query.trim());
  setParam(params, "project_id", filters.projectID);
  setParam(params, "connector_kind", filters.connectorKind);
  setParam(params, "status", filters.status);
  setParam(params, "source", filters.source);
  const target = targetItems.find((item) => item.ref === filters.targetRef);
  setParam(params, "target_id", target?.target_id);
  setParam(params, "profile_id", target?.profile_id);
  if (target?.runtime_id && !target.target_id && !target.profile_id) setParam(params, "runtime_id", target.runtime_id);
  setParam(params, "label_id", filters.labelID);
  return params.toString();
}

function setParam(params: URLSearchParams, name: string, value: string | number | null | undefined) {
  if (value) params.set(name, String(value));
}

function useHistoryReferences() {
  const [labels, setLabels] = useState<Resource<HistoryLabel>>({ state: "idle", data: [], error: null });
  const [targets, setTargets] = useState<Resource<HistoryTarget>>({ state: "idle", data: [], error: null });
  const [projects, setProjects] = useState<Resource<ProjectSummary>>({ state: "idle", data: [], error: null });
  const requests = useRequestGuard("history-references");
  async function loadLabels() {
    await loadReference("/api/history-labels", setLabels, historyLabelsResponse, requests);
  }
  async function loadTargets() {
    await loadReference("/api/history/targets", setTargets, historyTargetsResponse, requests);
  }
  async function loadProjects() {
    await loadReference("/api/projects", setProjects, projectListResponse, requests);
  }
  const loadForEffect = useEffectEvent(() => {
    void loadLabels();
    void loadTargets();
    void loadProjects();
  });
  useEffect(() => {
    loadForEffect();
  }, []);
  return { labels, targets, projects, loadLabels, loadTargets };
}

async function loadReference<T>(
  path: string,
  setState: Dispatch<SetStateAction<Resource<T>>>,
  selectData: (_value: unknown) => T[],
  requests: ReturnType<typeof useRequestGuard>,
) {
  const request = requests.begin(path);
  setState((current) => ({ ...current, state: "loading", error: null }));
  try {
    const data = await apiGet(path, { signal: request.signal });
    if (!request.isCurrent()) return;
    setState({ state: "ready", data: selectData(data), error: null });
  } catch (error) {
    if (request.isCurrent()) setState({ state: "error", data: [], error: errorMessage(error) });
  } finally {
    request.complete();
  }
}
