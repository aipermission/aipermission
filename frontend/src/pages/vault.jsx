import { Copy, KeyRound, Plus, RefreshCcw, RotateCw, Search, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button } from "../components/ui/button";
import { Dialog } from "../components/ui/dialog";
import { Field, Input, Select, Textarea } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { apiGet, apiPost, apiPut } from "../lib/api";
import { toLocalDateTime, toRFC3339 } from "../lib/date-time";
import { VaultBindingsDialog, VaultEditor, VaultRow, generatorKinds, selectedBinding } from "./vault-components";

const emptyEditor = {
  open: false,
  mode: "create",
  item: null,
  source: "imported",
  name: "",
  value: "",
  owner_project_id: "",
  shared_project_ids: [],
  secret_type: "generic_secret",
  generator_kind: "random_token",
  provider: "",
  environment: "",
  description: "",
  expires_at: "",
  expiry_warning_days: 14,
  tags: "",
  usage_notes: [],
};

const emptyReplace = {
  open: false,
  item: null,
  source: "imported",
  value: "",
  generator_kind: "random_token",
  preview_value: "",
  preview_token: "",
  preview_state: "idle",
  state: "idle",
  error: null,
};

export function VaultPage() {
  const [items, setItems] = useState({ state: "loading", data: [], total: 0, error: null });
  const [projects, setProjects] = useState({ state: "loading", data: [], error: null });
  const [filters, setFilters] = useState({ project_id: "", query: "", expiry: "all" });
  const [editor, setEditor] = useState(emptyEditor);
  const [reveal, setReveal] = useState({ open: false, item: null, state: "idle", value: "", error: null, copied: false });
  const [replace, setReplace] = useState(emptyReplace);
  const [remove, setRemove] = useState({ open: false, item: null, confirm: "", state: "idle", error: null });
  const [bindings, setBindings] = useState({
    open: false,
    item: null,
    state: "idle",
    data: [],
    targets: [],
    source_project_id: "",
    target_id: "",
    profile_id: "",
    replace_existing: false,
    error: null,
  });
  const [action, setAction] = useState({ state: "idle", message: "", error: null });
  const searchTimer = useRef(null);
  const itemsRequest = useRef(0);
  const revealRequest = useRef(0);
  const replacePreviewRequest = useRef(0);
  const bindingsRequest = useRef(0);

  const loadItems = useCallback(async () => {
    const requestID = ++itemsRequest.current;
    setItems((current) => ({ ...current, state: "loading", error: null }));
    try {
      const params = new URLSearchParams();
      if (filters.project_id) params.set("project_id", filters.project_id);
      if (filters.query.trim()) params.set("q", filters.query.trim());
      const data = await apiGet(`/api/vault-items${params.size ? `?${params}` : ""}`);
      if (requestID !== itemsRequest.current) return;
      setItems({ state: "ready", data: data.items || [], total: data.total || 0, error: null });
    } catch (error) {
      if (requestID !== itemsRequest.current) return;
      setItems({ state: "error", data: [], total: 0, error: error.message });
    }
  }, [filters.project_id, filters.query]);

  useEffect(() => {
    void loadProjects();
    return () => {
      clearTimeout(searchTimer.current);
      itemsRequest.current += 1;
    };
  }, []);

  useEffect(() => {
    clearTimeout(searchTimer.current);
    searchTimer.current = setTimeout(() => void loadItems(), 200);
    return () => clearTimeout(searchTimer.current);
  }, [loadItems]);

  useEffect(() => {
    if (!reveal.open || !reveal.value) return undefined;
    const timer = setTimeout(closeReveal, 30000);
    return () => clearTimeout(timer);
  }, [reveal.open, reveal.value]);

  useEffect(() => {
    if (!replace.open || !replace.preview_value) return undefined;
    const timer = setTimeout(() => {
      replacePreviewRequest.current += 1;
      setReplace((current) => ({
        ...current,
        preview_value: "",
        preview_token: "",
        preview_state: "idle",
      }));
    }, 30000);
    return () => clearTimeout(timer);
  }, [replace.open, replace.preview_value]);

  const visibleItems = useMemo(() => {
    const now = Date.now();
    return items.data.filter((item) => {
      if (filters.expiry === "expired") return item.expires_at && Date.parse(item.expires_at) <= now;
      if (filters.expiry === "warning") {
        if (!item.expires_at) return false;
        const warningAt = Date.parse(item.expires_at) - Number(item.expiry_warning_days || 14) * 86400000;
        return Date.parse(item.expires_at) > now && warningAt <= now;
      }
      if (filters.expiry === "none") return !item.expires_at;
      return true;
    });
  }, [items.data, filters.expiry]);

  async function loadProjects() {
    setProjects((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/projects");
      setProjects({ state: "ready", data: data.items || [], error: null });
    } catch (error) {
      setProjects({ state: "error", data: [], error: error.message });
    }
  }

  function openCreate() {
    const owner = filters.project_id || projects.data.find((project) => project.slug !== "ungrouped")?.id || projects.data[0]?.id || "";
    setAction({ state: "idle", message: "", error: null });
    setEditor({ ...emptyEditor, open: true, owner_project_id: owner });
  }

  function openEdit(item) {
    setAction({ state: "idle", message: "", error: null });
    setEditor({
      ...emptyEditor,
      open: true,
      mode: "edit",
      item,
      source: item.source,
      name: item.name,
      owner_project_id: item.owner_project_id,
      shared_project_ids: item.project_ids || [],
      secret_type: item.secret_type,
      generator_kind: item.generator_kind || "random_token",
      provider: item.provider || "",
      environment: item.environment || "",
      description: item.description || "",
      expires_at: toLocalDateTime(item.expires_at),
      expiry_warning_days: item.expiry_warning_days || 14,
      tags: (item.tags || []).join(", "),
      usage_notes: (item.usage_notes || []).map((note) => ({ location: note.location, notes: note.notes })),
    });
  }

  function closeEditor() {
    setEditor(emptyEditor);
  }

  async function saveItem(event) {
    event.preventDefault();
    setAction({ state: "saving", message: "", error: null });
    const common = {
      name: editor.name.trim().toUpperCase(),
      owner_project_id: Number(editor.owner_project_id),
      shared_project_ids: editor.shared_project_ids.map(Number),
      secret_type: editor.secret_type,
      provider: editor.provider,
      environment: editor.environment,
      description: editor.description,
      expires_at: toRFC3339(editor.expires_at),
      expiry_warning_days: Number(editor.expiry_warning_days),
      tags: splitTags(editor.tags),
      usage_notes: editor.usage_notes
        .filter((note) => note.location.trim())
        .map((note) => ({ location: note.location, notes: note.notes })),
    };
    try {
      if (editor.mode === "edit") {
        await apiPut(`/api/vault-items/${editor.item.id}`, {
          ...common,
          expected_metadata_revision: editor.item.metadata_revision,
        });
      } else {
        await apiPost("/api/vault-items", {
          ...common,
          source: editor.source,
          value: editor.source === "imported" ? editor.value : "",
          generator_kind: editor.source === "generated" ? editor.generator_kind : "",
        });
      }
      closeEditor();
      setAction({ state: "ready", message: editor.mode === "edit" ? "Vault item updated." : "Vault item created.", error: null });
      await loadItems();
    } catch (error) {
      setAction({ state: "error", message: "", error: error.message });
    }
  }

  async function openReveal(item) {
    const requestID = ++revealRequest.current;
    setReveal({ open: true, item, state: "loading", value: "", error: null, copied: false });
    try {
      const data = await apiPost(`/api/vault-items/${item.id}/reveal`, {});
      if (requestID !== revealRequest.current) return;
      setReveal({ open: true, item, state: "ready", value: data.value || "", error: null, copied: false });
    } catch (error) {
      if (requestID !== revealRequest.current) return;
      setReveal({ open: true, item, state: "error", value: "", error: error.message, copied: false });
    }
  }

  function closeReveal() {
    revealRequest.current += 1;
    setReveal({ open: false, item: null, state: "idle", value: "", error: null, copied: false });
  }

  async function copyRevealedValue() {
    if (!reveal.value) return;
    try {
      await navigator.clipboard.writeText(reveal.value);
      setReveal((current) => ({ ...current, copied: true }));
    } catch {
      setReveal((current) => ({ ...current, error: "Clipboard access failed." }));
    }
  }

  async function replaceValue(event) {
    event.preventDefault();
    if (!replace.item) return;
    setReplace((current) => ({ ...current, state: "saving", error: null }));
    try {
      await apiPost(`/api/vault-items/${replace.item.id}/value`, {
        source: replace.source,
        value: replace.source === "imported" ? replace.value : "",
        generator_kind: replace.source === "generated" ? replace.generator_kind : "",
        preview_token: replace.source === "generated" ? replace.preview_token : "",
        expected_value_version: replace.item.value_version,
      });
      setReplace(emptyReplace);
      setAction({
        state: "ready",
        message:
          replace.source === "generated"
            ? "A new local Vault value was generated. Provider-side credentials were not rotated."
            : "Local Vault value replaced. Provider-side credentials were not rotated.",
        error: null,
      });
      await loadItems();
    } catch (error) {
      setReplace((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  async function generateReplacementPreview(item, generatorKind) {
    if (!item || !generatorKind) return;
    const requestID = ++replacePreviewRequest.current;
    setReplace((current) => ({
      ...current,
      source: "generated",
      generator_kind: generatorKind,
      value: "",
      preview_value: "",
      preview_token: "",
      preview_state: "loading",
      error: null,
    }));
    try {
      const data = await apiPost(`/api/vault-items/${item.id}/generate-preview`, {
        generator_kind: generatorKind,
      });
      if (requestID !== replacePreviewRequest.current) return;
      setReplace((current) => ({
        ...current,
        preview_value: data.value || "",
        preview_token: data.preview_token || "",
        preview_state: "ready",
        error: null,
      }));
    } catch (error) {
      if (requestID !== replacePreviewRequest.current) return;
      setReplace((current) => ({
        ...current,
        preview_value: "",
        preview_token: "",
        preview_state: "error",
        error: error.message,
      }));
    }
  }

  function closeReplace() {
    replacePreviewRequest.current += 1;
    setReplace(emptyReplace);
  }

  async function deleteItem() {
    if (!remove.item) return;
    setRemove((current) => ({ ...current, state: "deleting", error: null }));
    try {
      await apiPost(`/api/vault-items/${remove.item.id}/delete`, {
        expected_value_version: remove.item.value_version,
        expected_metadata_revision: remove.item.metadata_revision,
      });
      setRemove({ open: false, item: null, confirm: "", state: "idle", error: null });
      setAction({ state: "ready", message: "Vault item deleted from the active database.", error: null });
      await loadItems();
    } catch (error) {
      setRemove((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  async function openBindings(item) {
    const requestID = ++bindingsRequest.current;
    setBindings({
      open: true,
      item,
      state: "loading",
      data: [],
      targets: [],
      source_project_id: String(item.owner_project_id),
      target_id: "",
      profile_id: "",
      replace_existing: false,
      error: null,
    });
    try {
      const [bindingData, inventory] = await Promise.all([
        apiGet(`/api/vault-default-bindings?vault_item_id=${item.id}`),
        apiGet("/api/connector-targets/inventory"),
      ]);
      if (requestID !== bindingsRequest.current) return;
      setBindings((current) => ({
        ...current,
        state: "ready",
        data: bindingData.items || [],
        targets: (inventory.items || [])
          .map((target) => ({
            ...target,
            profiles: (target.profiles || []).filter((profile) => profile.vault_session_supported),
          }))
          .filter((target) => target.profiles.length > 0),
      }));
    } catch (error) {
      if (requestID !== bindingsRequest.current) return;
      setBindings((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  function closeBindings() {
    bindingsRequest.current += 1;
    setBindings({
      open: false,
      item: null,
      state: "idle",
      data: [],
      targets: [],
      source_project_id: "",
      target_id: "",
      profile_id: "",
      replace_existing: false,
      error: null,
    });
  }

  async function saveBinding(event) {
    event.preventDefault();
    const current = selectedBinding(bindings);
    setBindings((value) => ({ ...value, state: "saving", error: null }));
    try {
      await apiPut("/api/vault-default-bindings", {
        vault_item_id: bindings.item.id,
        source_project_id: Number(bindings.source_project_id),
        target_id: Number(bindings.target_id),
        profile_id: Number(bindings.profile_id),
        replace_existing: bindings.replace_existing,
        expected_binding_revision: current?.binding_revision || 0,
      });
      const result = await apiGet(`/api/vault-default-bindings?vault_item_id=${bindings.item.id}`);
      setBindings((value) => ({ ...value, state: "ready", data: result.items || [], error: null }));
      setAction({ state: "ready", message: "Default session environment binding saved.", error: null });
    } catch (error) {
      setBindings((value) => ({ ...value, state: "error", error: error.message }));
    }
  }

  async function deleteBinding(item) {
    setBindings((value) => ({ ...value, state: "saving", error: null }));
    try {
      await apiPost(`/api/vault-default-bindings/${item.id}/delete`, {
        expected_binding_revision: item.binding_revision,
      });
      setBindings((value) => ({
        ...value,
        state: "ready",
        data: value.data.filter((binding) => binding.id !== item.id),
        error: null,
      }));
      setAction({ state: "ready", message: "Default session environment binding removed.", error: null });
    } catch (error) {
      setBindings((value) => ({ ...value, state: "error", error: error.message }));
    }
  }

  return (
    <section className="mx-auto grid w-full max-w-7xl gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold">Vault</h3>
          <p className="text-sm text-stone-500">Keep project secrets encrypted locally and ready for controlled connector sessions.</p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="outline" onClick={loadItems} disabled={items.state === "loading"}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
          <Button type="button" onClick={openCreate} disabled={projects.data.length === 0}>
            <Plus className="h-4 w-4" />
            Add vault item
          </Button>
        </div>
      </div>

      <div className="grid gap-3 border-y border-stone-200 py-4 md:grid-cols-[220px_minmax(0,1fr)_180px]">
        <Select value={filters.project_id} onChange={(event) => setFilters((current) => ({ ...current, project_id: event.target.value }))}>
          <option value="">All projects</option>
          {projects.data.map((project) => (
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
            onChange={(event) => setFilters((current) => ({ ...current, query: event.target.value }))}
            placeholder="Search name, provider, environment, or description"
          />
        </div>
        <Select value={filters.expiry} onChange={(event) => setFilters((current) => ({ ...current, expiry: event.target.value }))}>
          <option value="all">All expiry states</option>
          <option value="warning">Expiring soon</option>
          <option value="expired">Expired</option>
          <option value="none">No expiry</option>
        </Select>
      </div>

      {items.state === "error" ? <Notice tone="bad">{items.error}</Notice> : null}
      {projects.state === "error" ? <Notice tone="bad">{projects.error}</Notice> : null}
      {action.message ? <Notice tone="good">{action.message}</Notice> : null}
      {action.error && !editor.open ? <Notice tone="bad">{action.error}</Notice> : null}
      {items.state === "ready" && items.total > items.data.length ? (
        <Notice tone="warn">
          Showing {items.data.length} of {items.total} matching Vault items. Refine the project or search filter to reach items outside this
          bounded result.
        </Notice>
      ) : null}

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
                projects={projects.data}
                onEdit={() => openEdit(item)}
                onReveal={() => void openReveal(item)}
                onReplace={() => setReplace({ ...emptyReplace, open: true, item })}
                onBindings={() => void openBindings(item)}
                onDelete={() => setRemove({ open: true, item, confirm: "", state: "idle", error: null })}
              />
            ))}
          </tbody>
        </table>
        {items.state === "loading" ? (
          <div className="p-4">
            <Notice>Loading Vault metadata...</Notice>
          </div>
        ) : null}
        {items.state === "ready" && visibleItems.length === 0 ? (
          <div className="grid min-h-48 place-items-center p-6 text-center">
            <div>
              <KeyRound className="mx-auto h-6 w-6 text-stone-400" />
              <p className="mt-2 text-sm font-semibold">No Vault items match this view.</p>
              <p className="mt-1 text-xs text-stone-500">Add a local value or generate one without exposing it to an AI client.</p>
            </div>
          </div>
        ) : null}
      </div>

      <VaultEditor
        editor={editor}
        projects={projects.data}
        action={action}
        onChange={setEditor}
        onClose={closeEditor}
        onSubmit={saveItem}
      />

      <VaultBindingsDialog
        state={bindings}
        projects={projects.data}
        onChange={setBindings}
        onClose={closeBindings}
        onSave={saveBinding}
        onDelete={(item) => void deleteBinding(item)}
      />

      <Dialog
        open={reveal.open}
        title={`Reveal ${reveal.item?.name || "Vault item"}`}
        description="The value is visible only in this local dialog and clears after 30 seconds."
        onClose={closeReveal}
        size="lg"
        autoFocusClose={false}
      >
        <div className="grid gap-4">
          {reveal.state === "loading" ? <Notice>Decrypting after audit...</Notice> : null}
          {reveal.error ? <Notice tone="bad">{reveal.error}</Notice> : null}
          {reveal.value ? (
            <div className="grid gap-2">
              <Textarea readOnly autoFocus className="min-h-36 font-mono" value={reveal.value} />
              <div className="flex justify-end">
                <Button type="button" onClick={copyRevealedValue}>
                  <Copy className="h-4 w-4" />
                  {reveal.copied ? "Copied" : "Copy value"}
                </Button>
              </div>
            </div>
          ) : null}
        </div>
      </Dialog>

      <Dialog
        open={replace.open}
        title="Replace local value"
        description="Import a new value or preview a locally generated value before saving it. This does not rotate the credential at its provider."
        onClose={() => replace.state !== "saving" && closeReplace()}
        size="lg"
        autoFocusClose={false}
      >
        <form className="grid gap-4" onSubmit={replaceValue}>
          <div className="grid grid-cols-2 rounded-md border border-stone-300 p-1">
            <Button
              type="button"
              variant={replace.source === "imported" ? "default" : "ghost"}
              className="h-9"
              onClick={() => {
                replacePreviewRequest.current += 1;
                setReplace((current) => ({
                  ...current,
                  source: "imported",
                  preview_value: "",
                  preview_token: "",
                  preview_state: "idle",
                  error: null,
                }));
              }}
            >
              Import value
            </Button>
            <Button
              type="button"
              variant={replace.source === "generated" ? "default" : "ghost"}
              className="h-9"
              onClick={() => void generateReplacementPreview(replace.item, replace.generator_kind)}
            >
              Generate locally
            </Button>
          </div>
          {replace.source === "imported" ? (
            <Field>
              New value
              <Textarea
                autoFocus
                className="min-h-36 font-mono"
                value={replace.value}
                onChange={(event) => setReplace((current) => ({ ...current, value: event.target.value }))}
                required
              />
            </Field>
          ) : (
            <div className="grid gap-3">
              <Field>
                Generator
                <Select
                  autoFocus
                  value={replace.generator_kind}
                  disabled={replace.preview_state === "loading" || replace.state === "saving"}
                  onChange={(event) => void generateReplacementPreview(replace.item, event.target.value)}
                >
                  {generatorKinds.map(([value, label]) => (
                    <option key={value} value={value}>
                      {label}
                    </option>
                  ))}
                </Select>
              </Field>
              <Field>
                Generated value
                <Textarea
                  readOnly
                  className="min-h-28 font-mono"
                  value={replace.preview_value}
                  placeholder={
                    replace.preview_state === "loading" ? "Generating locally..." : "Choose Generate locally to create a preview."
                  }
                />
              </Field>
              <div className="flex items-center justify-between gap-3">
                <p className="text-xs text-stone-500">The displayed value clears after 30 seconds and is not saved until you confirm.</p>
                <Button
                  type="button"
                  variant="outline"
                  disabled={replace.preview_state === "loading" || replace.state === "saving"}
                  onClick={() => void generateReplacementPreview(replace.item, replace.generator_kind)}
                >
                  <RotateCw className="h-4 w-4" />
                  {replace.preview_state === "loading" ? "Generating..." : "Regenerate"}
                </Button>
              </div>
            </div>
          )}
          {replace.error ? <Notice tone="bad">{replace.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" disabled={replace.state === "saving"} onClick={closeReplace}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={
                (replace.source === "imported" && !replace.value) ||
                (replace.source === "generated" && !replace.preview_token) ||
                replace.state === "saving"
              }
            >
              {replace.state === "saving"
                ? "Replacing..."
                : replace.source === "generated"
                  ? "Save generated value"
                  : "Replace local value"}
            </Button>
          </div>
        </form>
      </Dialog>

      <Dialog
        open={remove.open}
        title="Delete Vault item"
        description="Deletion cannot erase existing backups, remote process environments, logs, or snapshots."
        onClose={() => setRemove({ open: false, item: null, confirm: "", state: "idle", error: null })}
        size="lg"
        autoFocusClose={false}
      >
        <div className="grid gap-4">
          <Notice tone="warn">
            Type <strong>{remove.item?.name}</strong> to permanently delete this item from the active database.
          </Notice>
          <Input
            autoFocus
            value={remove.confirm}
            onChange={(event) => setRemove((current) => ({ ...current, confirm: event.target.value }))}
          />
          {remove.error ? <Notice tone="bad">{remove.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button
              type="button"
              variant="outline"
              onClick={() => setRemove({ open: false, item: null, confirm: "", state: "idle", error: null })}
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="danger"
              disabled={remove.confirm !== remove.item?.name || remove.state === "deleting"}
              onClick={deleteItem}
            >
              <Trash2 className="h-4 w-4" />
              {remove.state === "deleting" ? "Deleting..." : "Delete Vault item"}
            </Button>
          </div>
        </div>
      </Dialog>
    </section>
  );
}

function splitTags(value) {
  return String(value || "")
    .split(",")
    .map((tag) => tag.trim())
    .filter(Boolean);
}
