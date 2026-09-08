import { RefreshCcw, Search } from "lucide-react";
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
import { useHistoryPageState } from "./use-history-page-state";

const statusOptions = [
  ["", "All statuses"],
  ["pending_approval", "Pending approval"],
  ["pending", "Pending"],
  ["running", "Running"],
  ["paused", "Paused"],
  ["completed", "Completed"],
  ["canceled", "Canceled"],
  ["stale", "Stale"],
  ["outcome_unknown", "Outcome unknown"],
  ["failed", "Failed"],
  ["declined", "Declined"],
  ["error", "Error"],
  ["untracked", "Not tracked"],
].map(([value, label]) => ({ value, label }));
const sourceOptions = [
  { value: "", label: "All sources" },
  { value: "mcp", label: "MCP" },
  { value: "manual", label: "Manual" },
  { value: "ui", label: "UI" },
];

export function HistoryPage() {
  const view = useHistoryPageState();
  const pageStart = view.state.total === 0 ? 0 : view.state.pageIndex * view.state.limit + 1;
  const pageEnd = Math.min(view.state.pageIndex * view.state.limit + view.state.data.length, view.state.total);
  return (
    <section className="mx-auto grid min-w-0 w-full max-w-7xl gap-5">
      <HistoryHeader state={view.state} onRefresh={view.refresh} />
      <HistoryStats stats={view.stats} />
      <HistoryFilters view={view} />
      <HistoryErrors view={view} />
      <HistoryTable state={view.state} onOpen={view.openHistoryItem} />
      <PaginationBar
        start={pageStart}
        end={pageEnd}
        total={view.state.total}
        disabled={view.state.state === "loading"}
        onPrevious={view.previous}
        onNext={view.next}
        hasPrevious={view.state.pageIndex > 0}
        hasNext={Boolean(view.state.nextCursor)}
      />
      <HistoryDialog
        item={view.selected}
        labels={view.labels.data}
        onClose={view.closeHistoryItem}
        onAttachLabel={view.attachLabel}
        onDetachLabel={view.detachLabel}
      />
    </section>
  );
}

function HistoryHeader({ state, onRefresh }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h1 className="text-lg font-semibold">History</h1>
        <p className="text-sm text-stone-500">Review every gateway activity through one connector-aware stream.</p>
      </div>
      <Button type="button" variant="outline" onClick={onRefresh} disabled={state.state === "loading"}>
        <RefreshCcw className="h-4 w-4" />
        Refresh
      </Button>
    </div>
  );
}

function HistoryStats({ stats }) {
  return (
    <div className="grid gap-3 md:grid-cols-4">
      <HistoryStat label="Total" value={stats.total} />
      <HistoryStat label="Shown" value={stats.shown} />
      <HistoryStat label="Active" value={stats.active} tone="warn" />
      <HistoryStat label="Failed/stale" value={stats.failed} tone="bad" />
    </div>
  );
}

function HistoryFilters({ view }) {
  const { filters, updateFilters, projects, connectorKindOptions, targetItems, labels } = view;
  const update = (field, value) => updateFilters((current) => ({ ...current, [field]: value }));
  return (
    <div className="grid gap-3 rounded-lg border border-stone-200 bg-white p-4">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-[1fr_.8fr_.8fr_.8fr_1.2fr_.9fr]">
        <Select
          aria-label="Filter by project"
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
        <FilterSelect
          label="Filter by connector type"
          value={filters.connectorKind}
          options={connectorKindOptions}
          onChange={(value) => update("connectorKind", value)}
        />
        <FilterSelect
          label="Filter by status"
          value={filters.status}
          options={statusOptions}
          onChange={(value) => update("status", value)}
        />
        <FilterSelect
          label="Filter by source"
          value={filters.source}
          options={sourceOptions}
          onChange={(value) => update("source", value)}
        />
        <Select aria-label="Filter by connector" value={filters.targetRef} onChange={(event) => update("targetRef", event.target.value)}>
          <option value="">All connectors</option>
          {targetItems
            .filter((target) => !filters.projectID || String(target.project_id) === String(filters.projectID))
            .map((target) => (
              <option key={target.ref} value={target.ref}>
                {targetOptionLabel(target)}
              </option>
            ))}
        </Select>
        <Select aria-label="Filter by label" value={filters.labelID} onChange={(event) => update("labelID", event.target.value)}>
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
            aria-label="Search history"
            value={filters.query}
            onChange={(event) => update("query", event.target.value)}
            placeholder="Search targets, actions, output, paths, or tokens"
            className="pl-9"
          />
        </div>
        <Button
          type="button"
          variant="outline"
          onClick={() => updateFilters({ query: "", projectID: "", connectorKind: "", status: "", source: "", targetRef: "", labelID: "" })}
        >
          Clear filters
        </Button>
      </div>
    </div>
  );
}

function FilterSelect({ label, value, options, onChange }) {
  return (
    <Select aria-label={label} value={value} onChange={(event) => onChange(event.target.value)}>
      {options.map((option) => (
        <option key={option.value || "all"} value={option.value}>
          {option.label}
        </option>
      ))}
    </Select>
  );
}

function HistoryErrors({ view }) {
  return (
    <>
      {view.state.state === "error" ? <Notice tone="bad">{view.state.error}</Notice> : null}
      {view.labels.state === "error" ? <Notice tone="bad">{view.labels.error}</Notice> : null}
      {view.projects.state === "error" ? <Notice tone="bad">{view.projects.error}</Notice> : null}
    </>
  );
}

function HistoryTable({ state, onOpen }) {
  return (
    <div data-testid="history-table-scroll" className="overflow-x-auto rounded-lg border border-stone-200 bg-white">
      <table className="w-full min-w-[960px] table-fixed border-collapse text-left text-sm">
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
          {state.state === "loading" ? <EmptyHistoryRow>Loading history...</EmptyHistoryRow> : null}
          {state.state === "ready" && state.data.length === 0 ? <EmptyHistoryRow>No history yet.</EmptyHistoryRow> : null}
          {state.state === "ready" ? state.data.map((item) => <HistoryRow key={item.id} item={item} onOpen={onOpen} />) : null}
        </tbody>
      </table>
    </div>
  );
}

function EmptyHistoryRow({ children }) {
  return (
    <tr>
      <td className="px-4 py-8 text-center text-sm text-stone-500" colSpan={7}>
        {children}
      </td>
    </tr>
  );
}

function HistoryRow({ item, onOpen }) {
  return (
    <tr className="transition hover:bg-stone-50">
      <td className="px-4 py-3">
        <StatusBadge status={item.status} />
      </td>
      <td className="px-4 py-3">
        <ConnectorBadge kind={item.connector_kind} />
      </td>
      <td className="truncate px-4 py-3">
        <button
          type="button"
          className="grid max-w-full text-left outline-none focus-visible:rounded focus-visible:ring-2 focus-visible:ring-emerald-600 focus-visible:ring-offset-2"
          aria-label={`Open history details for ${item.target_name || "unknown target"}`}
          onClick={() => onOpen(item)}
        >
          <span className="truncate font-medium text-stone-900 underline-offset-2 hover:underline">{item.target_name || "-"}</span>
          <span className="truncate text-xs text-stone-500">{[item.project_name, item.profile_label].filter(Boolean).join(" / ")}</span>
        </button>
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
  );
}
