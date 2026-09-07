import { KeyRound, Plus, RefreshCcw, Search } from "lucide-react";
import { Button } from "../ui/button";
import { Input, Select } from "../ui/form";
import { Notice } from "../ui/notice";
import { VaultRow } from "./vault-row";

export function VaultPageHeader({ loading, canCreate, onRefresh, onCreate }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h3 className="text-lg font-semibold">Vault</h3>
        <p className="text-sm text-stone-500">Keep project secrets encrypted locally and ready for controlled connector sessions.</p>
      </div>
      <div className="flex gap-2">
        <Button type="button" variant="outline" onClick={onRefresh} disabled={loading}>
          <RefreshCcw className="h-4 w-4" />
          Refresh
        </Button>
        <Button type="button" onClick={onCreate} disabled={!canCreate}>
          <Plus className="h-4 w-4" />
          Add vault item
        </Button>
      </div>
    </div>
  );
}

export function VaultFilters({ filters, projects, onChange }) {
  return (
    <div className="grid gap-3 border-y border-stone-200 py-4 md:grid-cols-[220px_minmax(0,1fr)_180px]">
      <Select value={filters.project_id} onChange={(event) => onChange({ project_id: event.target.value })}>
        <option value="">All projects</option>
        {projects.map((project) => (
          <option key={project.id} value={project.id}>
            {project.name}
          </option>
        ))}
      </Select>
      <div className="relative">
        <Search className="pointer-events-none absolute left-3 top-3 h-4 w-4 text-stone-400" />
        <Input
          className="pl-9"
          value={filters.query}
          onChange={(event) => onChange({ query: event.target.value })}
          placeholder="Search name, provider, environment, or description"
        />
      </div>
      <Select value={filters.expiry} onChange={(event) => onChange({ expiry: event.target.value })}>
        <option value="all">All expiry states</option>
        <option value="warning">Expiring soon</option>
        <option value="expired">Expired</option>
        <option value="none">No expiry</option>
      </Select>
    </div>
  );
}

export function VaultItemsTable({ items, visibleItems, projects, onEdit, onReveal, onReplace, onBindings, onDelete }) {
  return (
    <div className="min-w-0 overflow-x-auto rounded-lg border border-stone-200 bg-white">
      <table className="w-full min-w-[1100px] table-fixed border-collapse text-left text-sm">
        <thead className="bg-stone-50 text-xs uppercase text-stone-500">
          <tr>
            <th className="w-[22%] px-4 py-3 font-semibold">Name</th>
            <th className="w-[12%] px-4 py-3 font-semibold">Type</th>
            <th className="w-[18%] px-4 py-3 font-semibold">Projects</th>
            <th className="w-[12%] px-4 py-3 font-semibold">Expires</th>
            <th className="w-[12%] px-4 py-3 font-semibold">Last injected</th>
            <th className="w-[24%] px-4 py-3 text-right font-semibold">Actions</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-stone-200">
          {visibleItems.map((item) => (
            <VaultRow
              key={item.id}
              item={item}
              projects={projects}
              onEdit={() => onEdit(item)}
              onReveal={() => onReveal(item)}
              onReplace={() => onReplace(item)}
              onBindings={() => onBindings(item)}
              onDelete={() => onDelete(item)}
            />
          ))}
        </tbody>
      </table>
      {items.state === "loading" ? <TableNotice>Loading Vault metadata...</TableNotice> : null}
      {items.state === "ready" && visibleItems.length === 0 ? <VaultEmptyState /> : null}
    </div>
  );
}

function TableNotice({ children }) {
  return (
    <div className="p-4">
      <Notice>{children}</Notice>
    </div>
  );
}

function VaultEmptyState() {
  return (
    <div className="grid min-h-48 place-items-center p-6 text-center">
      <div>
        <KeyRound className="mx-auto h-6 w-6 text-stone-400" />
        <p className="mt-2 text-sm font-semibold">No Vault items match this view.</p>
        <p className="mt-1 text-xs text-stone-500">Add a local value or generate one without exposing it to an AI client.</p>
      </div>
    </div>
  );
}
