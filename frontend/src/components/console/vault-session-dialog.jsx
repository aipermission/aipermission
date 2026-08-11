import { KeyRound, Search, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Checkbox, Input, Select } from "../ui/form";
import { Notice } from "../ui/notice";
import { preferredDefaultBindings } from "../../lib/vault-session-selection";

export function VaultSessionDialog({ state, onClose, onStart }) {
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState({});
  const [projectID, setProjectID] = useState("");
  const items = useMemo(() => state.options?.items || [], [state.options?.items]);
  const defaults = useMemo(() => state.options?.defaults || [], [state.options?.defaults]);
  const projects = useMemo(() => state.options?.projects || [], [state.options?.projects]);
  const projectNames = useMemo(() => Object.fromEntries(projects.map((project) => [Number(project.id), project.name])), [projects]);
  const availableItems = useMemo(() => {
    const byID = new Map(items.map((item) => [Number(item.id), item]));
    defaults.forEach((binding) => {
      if (!byID.has(Number(binding.vault_item_id))) {
        byID.set(Number(binding.vault_item_id), {
          id: binding.vault_item_id,
          name: binding.vault_item_name,
          owner_project_id: binding.source_project_id,
          owner_project_name: binding.source_project_name,
          project_ids: [],
        });
      }
    });
    return [...byID.values()];
  }, [items, defaults]);

  useEffect(() => {
    if (!state.open) return;
    const next = {};
    preferredDefaultBindings(defaults, state.options?.target_project_id).forEach((binding) => {
      next[binding.vault_item_id] = {
        item_id: Number(binding.vault_item_id),
        source_project_id: Number(binding.source_project_id),
        replace_existing: Boolean(binding.replace_existing),
        binding_id: Number(binding.id),
        binding_revision: Number(binding.binding_revision),
      };
    });
    setSelected(next);
    setQuery("");
    setProjectID(String(state.options?.target_project_id || projects[0]?.id || ""));
  }, [state.open, state.runtime?.id, state.options?.target_project_id, defaults, projects]);

  const visibleItems = useMemo(() => {
    const needle = query.trim().toLowerCase();
    const selectedProjectID = Number(projectID);
    return availableItems.filter((item) => {
      const assignedProjectIDs = [Number(item.owner_project_id), ...(item.project_ids || []).map(Number)];
      if (!assignedProjectIDs.includes(selectedProjectID)) return false;
      return (
        !needle ||
        [item.name, item.provider, item.environment, item.description].some((value) =>
          String(value || "")
            .toLowerCase()
            .includes(needle),
        )
      );
    });
  }, [availableItems, projectID, query]);
  const selectedItems = useMemo(() => {
    const byID = new Map(availableItems.map((item) => [Number(item.id), item]));
    return Object.values(selected)
      .map((selection) => ({ selection, item: byID.get(Number(selection.item_id)) }))
      .filter((entry) => entry.item)
      .sort((left, right) => String(left.item.name).localeCompare(String(right.item.name)));
  }, [availableItems, selected]);

  function toggle(item) {
    setSelected((current) => {
      const next = { ...current };
      if (next[item.id]) {
        delete next[item.id];
        return next;
      }
      const defaultBinding = preferredDefaultBindings(defaults, projectID).find(
        (binding) => Number(binding.vault_item_id) === Number(item.id),
      );
      const selectedFromDefaultProject = Number(defaultBinding?.source_project_id) === Number(projectID);
      next[item.id] = selectedFromDefaultProject
        ? {
            item_id: Number(item.id),
            source_project_id: Number(defaultBinding.source_project_id),
            replace_existing: Boolean(defaultBinding.replace_existing),
            binding_id: Number(defaultBinding.id),
            binding_revision: Number(defaultBinding.binding_revision),
          }
        : {
            item_id: Number(item.id),
            source_project_id: Number(projectID),
            replace_existing: Boolean(defaultBinding?.replace_existing),
          };
      return next;
    });
  }

  function update(itemID, patch) {
    setSelected((current) => ({ ...current, [itemID]: { ...current[itemID], ...patch } }));
  }

  function removeSelection(itemID) {
    setSelected((current) => {
      const next = { ...current };
      delete next[itemID];
      return next;
    });
  }

  return (
    <Dialog
      open={state.open}
      title="Start session with Vault environment"
      description={`Choose secrets for ${state.runtime?.name || "this connector session"}. Values stay hidden and are applied only to the new session.`}
      onClose={onClose}
      size="xl"
      closeOnOverlay={false}
      closeDisabled={state.status === "starting"}
    >
      <div className="grid min-h-0 gap-4">
        <div className="grid gap-3 sm:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)]">
          <label className="grid gap-1.5 text-sm font-medium text-stone-700">
            Project
            <Select value={projectID} onChange={(event) => setProjectID(event.target.value)} autoFocus>
              {projects.map((project) => (
                <option key={project.id} value={project.id}>
                  {project.name}
                </option>
              ))}
            </Select>
          </label>
          <label className="grid gap-1.5 text-sm font-medium text-stone-700">
            Vault items
            <span className="relative">
              <Search className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-stone-400" />
              <Input
                className="w-full pl-9"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="Search this project"
              />
            </span>
          </label>
        </div>
        {selectedItems.length > 0 ? (
          <div className="dark-panel-subtle grid gap-2 rounded-md border border-stone-200 bg-stone-50 p-3">
            <p className="text-xs font-semibold uppercase text-stone-500">Selected for this session</p>
            <div className="flex flex-wrap gap-2">
              {selectedItems.map(({ item, selection }) => (
                <span
                  key={item.id}
                  className="inline-flex max-w-full items-center gap-2 rounded border border-stone-200 bg-white px-2 py-1 text-xs"
                >
                  <span className="truncate font-mono font-semibold">{item.name}</span>
                  <span className="truncate text-stone-500">
                    {projectNames[selection.source_project_id] || `Project ${selection.source_project_id}`}
                  </span>
                  <button
                    type="button"
                    className="text-stone-400 transition hover:text-red-600"
                    title={`Remove ${item.name}`}
                    aria-label={`Remove ${item.name}`}
                    onClick={() => removeSelection(item.id)}
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                </span>
              ))}
            </div>
          </div>
        ) : null}
        <div className="max-h-[52vh] overflow-y-auto rounded-md border border-stone-200">
          {visibleItems.map((item) => {
            const selection = selected[item.id];
            return (
              <div key={item.id} className="grid gap-3 border-b border-stone-200 p-3 last:border-b-0">
                <label className="flex min-w-0 items-start gap-3">
                  <Checkbox checked={Boolean(selection)} onChange={() => toggle(item)} />
                  <span className="min-w-0 flex-1">
                    <span className="flex flex-wrap items-center gap-2">
                      <span className="font-mono text-sm font-semibold">{item.name}</span>
                      {defaults.some((binding) => Number(binding.vault_item_id) === Number(item.id)) ? (
                        <Badge tone="good">default</Badge>
                      ) : null}
                    </span>
                    <span className="mt-1 block truncate text-xs text-stone-500">{item.description || item.provider || "Vault item"}</span>
                  </span>
                </label>
                {selection ? (
                  <div className="flex flex-wrap items-center justify-between gap-3 pl-7">
                    <span className="text-xs text-stone-500">
                      From {projectNames[selection.source_project_id] || `Project ${selection.source_project_id}`}
                    </span>
                    <label className="flex items-center gap-2 text-sm text-stone-700">
                      <Checkbox
                        checked={selection.replace_existing}
                        onChange={(event) =>
                          update(item.id, { replace_existing: event.target.checked, binding_id: undefined, binding_revision: undefined })
                        }
                      />
                      Overwrite existing shell value
                    </label>
                  </div>
                ) : null}
              </div>
            );
          })}
          {visibleItems.length === 0 ? (
            <p className="p-6 text-center text-sm text-stone-500">No matching Vault items in this project.</p>
          ) : null}
        </div>
        {Number(state.options?.total || 0) > items.length ? (
          <Notice tone="warn" className="py-2 text-xs">
            Showing {items.length} of {state.options.total} Vault items. Narrow the project first, then use the Vault page search to manage
            items outside this bounded session list.
          </Notice>
        ) : null}
        {Object.keys(selected).length > 0 ? (
          <Notice tone="warn" className="py-2 text-xs">
            Every process in the new shell can read, transform, persist, or transmit the selected values. Exact-value redaction only reduces
            accidental output exposure, and detached processes may retain inherited values.
          </Notice>
        ) : null}
        {state.error ? <Notice tone="bad">{state.error}</Notice> : null}
        <div className="flex items-center justify-between gap-3">
          <p className="text-xs text-stone-500">{Object.keys(selected).length} selected. Starting with none opens a normal session.</p>
          <Button type="button" onClick={() => onStart(Object.values(selected))} disabled={state.status === "starting"}>
            <KeyRound className="h-4 w-4" />
            {state.status === "starting" ? "Starting..." : "Start session"}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
