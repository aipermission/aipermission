import { RefreshCcw, Search } from "lucide-react";
import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { Button } from "../components/ui/button";
import { Input, Select } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { PaginationBar } from "../components/ui/pagination-bar";
import {
  ActionBadge,
  ConnectorBadge,
  HistoryDialog,
  HistoryStat,
  LabelPreview,
  StatusBadge,
  entrySummary,
  formatShortTime,
  targetOptionLabel,
} from "../components/history/history-components";
import { connectorKindLabel } from "../connectors/templates/common";
import { useRequestGuard } from "../connectors/templates/_shared/request-guard";
import { apiDelete, apiGet, apiPost } from "../lib/api";
import {
  currentHistoryPage,
  firstHistoryPage,
  nextHistoryPage,
  previousHistoryPage,
  resolvedHistoryTotal,
} from "../lib/history-pagination";

const statusOptions = [
  { value: "", label: "All statuses" },
  { value: "pending_approval", label: "Pending approval" },
  { value: "pending", label: "Pending" },
  { value: "running", label: "Running" },
  { value: "paused", label: "Paused" },
  { value: "completed", label: "Completed" },
  { value: "canceled", label: "Canceled" },
  { value: "stale", label: "Stale" },
  { value: "outcome_unknown", label: "Outcome unknown" },
  { value: "failed", label: "Failed" },
  { value: "declined", label: "Declined" },
  { value: "error", label: "Error" },
  { value: "untracked", label: "Not tracked" },
];

const sourceOptions = [
  { value: "", label: "All sources" },
  { value: "mcp", label: "MCP" },
  { value: "manual", label: "Manual" },
  { value: "ui", label: "UI" },
];

export function HistoryPage() {
  const [filters, setFilters] = useState({
    query: "",
    projectID: "",
    connectorKind: "",
    status: "",
    source: "",
    targetRef: "",
    labelID: "",
  });
  const [state, setState] = useState({
    state: "idle",
    data: [],
    total: 0,
    ...firstHistoryPage(50),
    nextCursor: null,
    error: null,
  });
  const [labels, setLabels] = useState({ state: "idle", data: [], error: null });
  const [targets, setTargets] = useState({ state: "idle", data: [], error: null });
  const [projects, setProjects] = useState({ state: "idle", data: [], error: null });
  const [selected, setSelected] = useState(null);
  const targetItems = useMemo(() => targets.data || [], [targets.data]);
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
  const loadHistoryForEffect = useEffectEvent((page, options) => loadHistory(page, options));

  useEffect(() => {
    void loadLabels();
    void loadHistoryTargets();
    void loadProjects();
  }, []);

  const connectorKindOptions = useMemo(() => {
    const kinds = Array.from(new Set(targetItems.map((target) => target.connector_kind).filter(Boolean))).sort();
    return [{ value: "", label: "All connectors" }, ...kinds.map((kind) => ({ value: kind, label: connectorKindLabel(kind) }))];
  }, [targetItems]);

  useEffect(() => {
    const generation = ++filterGenerationRef.current;
    filterTransitionPendingRef.current = true;
    requestGuard.invalidate("list");
    requestGuard.invalidate("poll");
    setState((current) => ({
      ...current,
      ...firstHistoryPage(current.limit),
      nextCursor: null,
    }));
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
    let timer = null;
    const poll = async () => {
      if (!filterTransitionPendingRef.current && !interactiveRequestPendingRef.current) {
        await loadHistoryForEffect(
          {
            limit: state.limit,
            cursor: state.cursor,
            pageIndex: state.pageIndex,
            cursorStack: state.cursorStack,
          },
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

  const stats = useMemo(() => {
    const data = state.data;
    return {
      total: state.total,
      shown: data.length,
      active: data.filter((item) => ["pending", "pending_approval", "running", "paused"].includes(item.status)).length,
      failed: data.filter((item) => ["failed", "error", "stale", "outcome_unknown"].includes(item.status)).length,
    };
  }, [state.data, state.total]);

  async function loadLabels() {
    setLabels((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/history-labels");
      setLabels({ state: "ready", data: data || [], error: null });
    } catch (error) {
      setLabels({ state: "error", data: [], error: error.message });
    }
  }

  async function loadHistoryTargets() {
    setTargets((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/history/targets");
      setTargets({ state: "ready", data: data.items || [], error: null });
    } catch (error) {
      setTargets({ state: "error", data: [], error: error.message });
    }
  }

  async function loadProjects() {
    setProjects((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/projects");
      setProjects({ state: "ready", data: data.items || [], error: null });
    } catch (error) {
      setProjects({ state: "error", data: [], error: error.message });
    }
  }

  async function loadHistory(page = currentHistoryPage(state), options = {}) {
    const channel = options.poll ? "poll" : "list";
    if (!options.poll) {
      interactiveRequestPendingRef.current = true;
      requestGuard.invalidate("poll");
    }
    const request = requestGuard.begin(channel);
    if (!options.silent) {
      setState((current) => ({
        ...current,
        state: "loading",
        cursor: page.cursor,
        pageIndex: page.pageIndex,
        cursorStack: page.cursorStack,
        error: null,
      }));
    }
    const params = new URLSearchParams({
      limit: String(page.limit),
    });
    if (page.cursor) params.set("cursor", page.cursor);
    if (options.includeTotal !== undefined) params.set("include_total", String(options.includeTotal));
    if (filters.query.trim()) params.set("q", filters.query.trim());
    if (filters.projectID) params.set("project_id", filters.projectID);
    if (filters.connectorKind) params.set("connector_kind", filters.connectorKind);
    if (filters.status) params.set("status", filters.status);
    if (filters.source) params.set("source", filters.source);
    const selectedTarget = targetItems.find((target) => target.ref === filters.targetRef);
    if (selectedTarget?.target_id) {
      params.set("target_id", String(selectedTarget.target_id));
    }
    if (selectedTarget?.profile_id) {
      params.set("profile_id", String(selectedTarget.profile_id));
    }
    if (selectedTarget?.runtime_id && !selectedTarget?.target_id && !selectedTarget?.profile_id) {
      params.set("runtime_id", String(selectedTarget.runtime_id));
    }
    if (filters.labelID) params.set("label_id", filters.labelID);
    try {
      const data = await apiGet(`/api/history?${params.toString()}`);
      if (!request.isCurrent()) return;
      setState((current) => ({
        state: "ready",
        data: data.items || [],
        total: resolvedHistoryTotal(current.total, data, page),
        limit: data.limit || page.limit,
        cursor: page.cursor,
        pageIndex: page.pageIndex,
        cursorStack: page.cursorStack,
        nextCursor: data.next_cursor || null,
        error: null,
      }));
    } catch (error) {
      if (!request.isCurrent()) return;
      setState((current) => ({ ...current, state: "error", data: [], total: 0, error: error.message }));
    } finally {
      if (!options.poll && request.isCurrent()) interactiveRequestPendingRef.current = false;
    }
  }

  function updateFilters(updater) {
    filterGenerationRef.current += 1;
    filterTransitionPendingRef.current = true;
    requestGuard.invalidate("list");
    requestGuard.invalidate("poll");
    setState((current) => ({ ...current, ...firstHistoryPage(current.limit), nextCursor: null }));
    setFilters(updater);
  }

  async function openHistoryItem(item) {
    const request = requestGuard.begin("detail");
    setSelected(item);
    try {
      const detail = await apiGet(`/api/history/${item.id}`);
      if (request.isCurrent()) setSelected(detail);
    } catch {
      if (request.isCurrent()) setSelected(item);
    }
  }

  function closeHistoryItem() {
    requestGuard.invalidate("detail");
    setSelected(null);
  }

  function updateItemLabels(id, nextLabels) {
    setSelected((current) => (current?.id === id ? { ...current, labels: nextLabels } : current));
    setState((current) => ({
      ...current,
      data: current.data.map((item) => (item.id === id ? { ...item, labels: nextLabels } : item)),
    }));
  }

  async function attachLabel(id, payload) {
    const nextLabels = await apiPost(`/api/history/${id}/labels`, payload);
    updateItemLabels(id, nextLabels || []);
    await loadLabels();
  }

  async function detachLabel(id, labelID) {
    const nextLabels = await apiDelete(`/api/history/${id}/labels/${labelID}`);
    updateItemLabels(id, nextLabels || []);
    if (filters.labelID && String(labelID) === String(filters.labelID)) {
      setState((current) => ({
        ...current,
        data: current.data.filter((item) => item.id !== id),
        total: Math.max(0, current.total - 1),
      }));
    }
  }

  const pageStart = state.total === 0 ? 0 : state.pageIndex * state.limit + 1;
  const pageEnd = Math.min(state.pageIndex * state.limit + state.data.length, state.total);

  return (
    <section className="mx-auto grid w-full max-w-7xl gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold">History</h3>
          <p className="text-sm text-stone-500">Review every gateway activity through one connector-aware stream.</p>
        </div>
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            void loadHistoryTargets();
            void loadHistory(currentHistoryPage(state), { includeTotal: true });
          }}
          disabled={state.state === "loading"}
        >
          <RefreshCcw className="h-4 w-4" />
          Refresh
        </Button>
      </div>

      <div className="grid gap-3 md:grid-cols-4">
        <HistoryStat label="Total" value={stats.total} />
        <HistoryStat label="Shown" value={stats.shown} />
        <HistoryStat label="Active" value={stats.active} tone="warn" />
        <HistoryStat label="Failed/stale" value={stats.failed} tone="bad" />
      </div>

      <div className="grid gap-3 rounded-lg border border-stone-200 bg-white p-4">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-[1fr_.8fr_.8fr_.8fr_1.2fr_.9fr]">
          <Select
            value={filters.projectID}
            onChange={(event) => updateFilters((current) => ({ ...current, projectID: event.target.value, targetRef: "" }))}
          >
            <option value="">All projects</option>
            {projects.data.map((project) => (
              <option key={project.id} value={project.id}>
                {project.name}
              </option>
            ))}
          </Select>
          <Select
            value={filters.connectorKind}
            onChange={(event) => updateFilters((current) => ({ ...current, connectorKind: event.target.value }))}
          >
            {connectorKindOptions.map((option) => (
              <option key={option.value || "all"} value={option.value}>
                {option.label}
              </option>
            ))}
          </Select>
          <Select value={filters.status} onChange={(event) => updateFilters((current) => ({ ...current, status: event.target.value }))}>
            {statusOptions.map((option) => (
              <option key={option.value || "all"} value={option.value}>
                {option.label}
              </option>
            ))}
          </Select>
          <Select value={filters.source} onChange={(event) => updateFilters((current) => ({ ...current, source: event.target.value }))}>
            {sourceOptions.map((option) => (
              <option key={option.value || "all"} value={option.value}>
                {option.label}
              </option>
            ))}
          </Select>
          <Select
            value={filters.targetRef}
            onChange={(event) => updateFilters((current) => ({ ...current, targetRef: event.target.value }))}
          >
            <option value="">All connectors</option>
            {targetItems
              .filter((target) => !filters.projectID || String(target.project_id) === String(filters.projectID))
              .map((target) => (
                <option key={target.ref} value={target.ref}>
                  {targetOptionLabel(target)}
                </option>
              ))}
          </Select>
          <Select value={filters.labelID} onChange={(event) => updateFilters((current) => ({ ...current, labelID: event.target.value }))}>
            <option value="">All labels</option>
            {labels.data.map((label) => (
              <option key={label.id} value={label.id}>
                {label.name}
              </option>
            ))}
          </Select>
        </div>
        <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-3">
          <div className="relative">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-stone-400" />
            <Input
              value={filters.query}
              onChange={(event) => updateFilters((current) => ({ ...current, query: event.target.value }))}
              placeholder="Search targets, actions, output, paths, or tokens"
              className="pl-9"
            />
          </div>
          <Button
            type="button"
            variant="outline"
            onClick={() =>
              updateFilters({ query: "", projectID: "", connectorKind: "", status: "", source: "", targetRef: "", labelID: "" })
            }
          >
            Clear filters
          </Button>
        </div>
      </div>

      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      {labels.state === "error" ? <Notice tone="bad">{labels.error}</Notice> : null}
      {projects.state === "error" ? <Notice tone="bad">{projects.error}</Notice> : null}

      <div className="overflow-hidden rounded-lg border border-stone-200 bg-white">
        <table className="w-full table-fixed border-collapse text-left text-sm">
          <thead className="bg-stone-50 text-xs uppercase text-stone-500">
            <tr>
              <th className="w-[12%] px-4 py-3 font-semibold">Status</th>
              <th className="w-[12%] px-4 py-3 font-semibold">Connector</th>
              <th className="w-[22%] px-4 py-3 font-semibold">Target</th>
              <th className="w-[14%] px-4 py-3 font-semibold">Action</th>
              <th className="w-[20%] px-4 py-3 font-semibold">Summary</th>
              <th className="w-[10%] px-4 py-3 font-semibold">Labels</th>
              <th className="w-[10%] px-4 py-3 text-right font-semibold">Time</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-stone-100">
            {state.state === "loading" ? (
              <tr>
                <td className="px-4 py-8 text-center text-sm text-stone-500" colSpan={7}>
                  Loading history...
                </td>
              </tr>
            ) : null}
            {state.state !== "loading" && state.data.length === 0 ? (
              <tr>
                <td className="px-4 py-8 text-center text-sm text-stone-500" colSpan={7}>
                  No history yet.
                </td>
              </tr>
            ) : null}
            {state.state !== "loading"
              ? state.data.map((item) => (
                  <tr key={item.id} className="cursor-pointer transition hover:bg-stone-50" onClick={() => openHistoryItem(item)}>
                    <td className="px-4 py-3">
                      <StatusBadge status={item.status} />
                    </td>
                    <td className="px-4 py-3">
                      <ConnectorBadge kind={item.connector_kind} />
                    </td>
                    <td className="truncate px-4 py-3">
                      <div className="truncate font-medium text-stone-900">{item.target_name || "-"}</div>
                      <div className="truncate text-xs text-stone-500">
                        {[item.project_name, item.profile_label].filter(Boolean).join(" / ")}
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <ActionBadge item={item} />
                    </td>
                    <td className="truncate px-4 py-3 text-stone-700">{entrySummary(item)}</td>
                    <td className="px-4 py-3">
                      <LabelPreview labels={item.labels || []} />
                    </td>
                    <td className="px-4 py-3 text-right text-xs text-stone-500">{formatShortTime(item.created_at)}</td>
                  </tr>
                ))
              : null}
          </tbody>
        </table>
      </div>

      <PaginationBar
        start={pageStart}
        end={pageEnd}
        total={state.total}
        disabled={state.state === "loading"}
        onPrevious={() => {
          const previous = previousHistoryPage(state);
          if (previous) void loadHistory(previous);
        }}
        onNext={() => {
          const next = nextHistoryPage(state);
          if (next) void loadHistory(next);
        }}
        hasPrevious={state.pageIndex > 0}
        hasNext={Boolean(state.nextCursor)}
      />

      <HistoryDialog
        item={selected}
        labels={labels.data}
        onClose={closeHistoryItem}
        onAttachLabel={attachLabel}
        onDetachLabel={detachLabel}
      />
    </section>
  );
}
