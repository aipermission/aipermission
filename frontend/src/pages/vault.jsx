import { Copy, Edit3, Eye, KeyRound, Plus, RefreshCcw, RotateCw, Search, Trash2, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Dialog } from "../components/ui/dialog";
import { Drawer } from "../components/ui/drawer";
import { Checkbox, Field, Input, Select, Textarea } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { apiGet, apiPost, apiPut } from "../lib/api";

const secretTypes = [
  ["generic_secret", "Generic secret"],
  ["api_key", "API key"],
  ["access_token", "Access token"],
  ["password", "Password"],
  ["client_secret", "Client secret"],
  ["webhook_hmac", "Webhook / HMAC secret"],
  ["connection", "Connection string"],
];

const generatorKinds = [
  ["random_token", "Random token (32 bytes)"],
  ["hex_secret", "Hex secret (32 bytes)"],
  ["password", "Password (32 characters)"],
  ["long_hmac_secret", "Long HMAC secret (64 bytes)"],
  ["uuid_v4", "UUID v4 (identifier)"],
];

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

export function VaultPage() {
  const [items, setItems] = useState({ state: "loading", data: [], total: 0, error: null });
  const [projects, setProjects] = useState({ state: "loading", data: [], error: null });
  const [filters, setFilters] = useState({ project_id: "", query: "", expiry: "all" });
  const [editor, setEditor] = useState(emptyEditor);
  const [reveal, setReveal] = useState({ open: false, item: null, state: "idle", value: "", error: null, copied: false });
  const [replace, setReplace] = useState({ open: false, item: null, value: "", state: "idle", error: null });
  const [remove, setRemove] = useState({ open: false, item: null, confirm: "", state: "idle", error: null });
  const [action, setAction] = useState({ state: "idle", message: "", error: null });
  const searchTimer = useRef(null);

  useEffect(() => {
    void Promise.all([loadProjects(), loadItems()]);
    return () => clearTimeout(searchTimer.current);
  }, []);

  useEffect(() => {
    clearTimeout(searchTimer.current);
    searchTimer.current = setTimeout(() => void loadItems(), 200);
    return () => clearTimeout(searchTimer.current);
  }, [filters.project_id, filters.query]);

  useEffect(() => {
    if (!reveal.open || !reveal.value) return undefined;
    const timer = setTimeout(closeReveal, 30000);
    return () => clearTimeout(timer);
  }, [reveal.open, reveal.value]);

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

  async function loadItems() {
    setItems((current) => ({ ...current, state: "loading", error: null }));
    try {
      const params = new URLSearchParams();
      if (filters.project_id) params.set("project_id", filters.project_id);
      if (filters.query.trim()) params.set("q", filters.query.trim());
      const data = await apiGet(`/api/vault-items${params.size ? `?${params}` : ""}`);
      setItems({ state: "ready", data: data.items || [], total: data.total || 0, error: null });
    } catch (error) {
      setItems({ state: "error", data: [], total: 0, error: error.message });
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
      usage_notes: editor.usage_notes.filter((note) => note.location.trim()).map((note) => ({ location: note.location, notes: note.notes })),
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
    setReveal({ open: true, item, state: "loading", value: "", error: null, copied: false });
    try {
      const data = await apiPost(`/api/vault-items/${item.id}/reveal`, {});
      setReveal({ open: true, item, state: "ready", value: data.value || "", error: null, copied: false });
    } catch (error) {
      setReveal({ open: true, item, state: "error", value: "", error: error.message, copied: false });
    }
  }

  function closeReveal() {
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
        value: replace.value,
        expected_value_version: replace.item.value_version,
      });
      setReplace({ open: false, item: null, value: "", state: "idle", error: null });
      setAction({ state: "ready", message: "Local Vault value replaced. Provider-side credentials were not rotated.", error: null });
      await loadItems();
    } catch (error) {
      setReplace((current) => ({ ...current, state: "error", error: error.message }));
    }
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
          {projects.data.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}
        </Select>
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-3 h-4 w-4 text-stone-400" />
          <Input className="pl-9" value={filters.query} onChange={(event) => setFilters((current) => ({ ...current, query: event.target.value }))} placeholder="Search name, provider, environment, or description" />
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

      <div className="min-w-0 overflow-x-auto rounded-lg border border-stone-200 bg-white">
        <table className="w-full min-w-[960px] table-fixed border-collapse text-left text-sm">
          <thead className="bg-stone-50 text-xs uppercase text-stone-500">
            <tr>
              <th className="w-[24%] px-4 py-3 font-semibold">Name</th>
              <th className="w-[15%] px-4 py-3 font-semibold">Type</th>
              <th className="w-[25%] px-4 py-3 font-semibold">Projects</th>
              <th className="w-[14%] px-4 py-3 font-semibold">Expires</th>
              <th className="w-[10%] px-4 py-3 font-semibold">Last injected</th>
              <th className="w-[12%] px-4 py-3 text-right font-semibold">Actions</th>
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
                onReplace={() => setReplace({ open: true, item, value: "", state: "idle", error: null })}
                onDelete={() => setRemove({ open: true, item, confirm: "", state: "idle", error: null })}
              />
            ))}
          </tbody>
        </table>
        {items.state === "loading" ? <div className="p-4"><Notice>Loading Vault metadata...</Notice></div> : null}
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

      <VaultEditor editor={editor} projects={projects.data} action={action} onChange={setEditor} onClose={closeEditor} onSubmit={saveItem} />

      <Dialog open={reveal.open} title={`Reveal ${reveal.item?.name || "Vault item"}`} description="The value is visible only in this local dialog and clears after 30 seconds." onClose={closeReveal} size="lg" autoFocusClose={false}>
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

      <Dialog open={replace.open} title="Replace local value" description="This updates AIPermission only. It does not rotate the credential at its provider." onClose={() => setReplace({ open: false, item: null, value: "", state: "idle", error: null })} size="lg" autoFocusClose={false}>
        <form className="grid gap-4" onSubmit={replaceValue}>
          <Field>
            New value
            <Textarea autoFocus className="min-h-36 font-mono" value={replace.value} onChange={(event) => setReplace((current) => ({ ...current, value: event.target.value }))} required />
          </Field>
          {replace.error ? <Notice tone="bad">{replace.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={() => setReplace({ open: false, item: null, value: "", state: "idle", error: null })}>Cancel</Button>
            <Button type="submit" disabled={!replace.value || replace.state === "saving"}>{replace.state === "saving" ? "Replacing..." : "Replace local value"}</Button>
          </div>
        </form>
      </Dialog>

      <Dialog open={remove.open} title="Delete Vault item" description="Deletion cannot erase existing backups, remote process environments, logs, or snapshots." onClose={() => setRemove({ open: false, item: null, confirm: "", state: "idle", error: null })} size="lg" autoFocusClose={false}>
        <div className="grid gap-4">
          <Notice tone="warn">Type <strong>{remove.item?.name}</strong> to permanently delete this item from the active database.</Notice>
          <Input autoFocus value={remove.confirm} onChange={(event) => setRemove((current) => ({ ...current, confirm: event.target.value }))} />
          {remove.error ? <Notice tone="bad">{remove.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={() => setRemove({ open: false, item: null, confirm: "", state: "idle", error: null })}>Cancel</Button>
            <Button type="button" variant="danger" disabled={remove.confirm !== remove.item?.name || remove.state === "deleting"} onClick={deleteItem}>
              <Trash2 className="h-4 w-4" />
              {remove.state === "deleting" ? "Deleting..." : "Delete Vault item"}
            </Button>
          </div>
        </div>
      </Dialog>
    </section>
  );
}

function VaultRow({ item, projects, onEdit, onReveal, onReplace, onDelete }) {
  const projectNames = [item.owner_project_name, ...(item.project_ids || []).map((id) => projects.find((project) => Number(project.id) === Number(id))?.name).filter(Boolean)];
  const expiry = expiryState(item);
  return (
    <tr className="hover:bg-stone-50">
      <td className="px-4 py-3">
        <p className="truncate font-mono text-xs font-semibold text-stone-950">{item.name}</p>
        <p className="mt-1 truncate text-xs text-stone-500">{[item.provider, item.environment].filter(Boolean).join(" / ") || "Local secret"}</p>
      </td>
      <td className="px-4 py-3"><Badge tone="neutral">{secretTypeLabel(item.secret_type)}</Badge></td>
      <td className="px-4 py-3">
        <div className="flex flex-wrap gap-1">
          {projectNames.map((name, index) => <Badge key={`${name}-${index}`} tone={index === 0 ? "good" : "neutral"}>{name}</Badge>)}
        </div>
      </td>
      <td className="px-4 py-3">
        <Badge tone={expiry.tone}>{expiry.label}</Badge>
      </td>
      <td className="px-4 py-3 text-xs text-stone-500">{item.last_used_at ? timeAgo(item.last_used_at) : "Never"}</td>
      <td className="px-4 py-3">
        <div className="flex justify-end gap-1">
          <IconButton title="Reveal and copy" icon={Eye} onClick={onReveal} />
          <IconButton title="Edit metadata" icon={Edit3} onClick={onEdit} />
          <IconButton title="Replace local value" icon={RotateCw} onClick={onReplace} />
          <IconButton title="Delete" icon={Trash2} onClick={onDelete} danger />
        </div>
      </td>
    </tr>
  );
}

function IconButton({ title, icon: Icon, onClick, danger = false }) {
  return (
    <Button type="button" variant="ghost" className={`h-9 w-9 px-0 ${danger ? "text-red-700" : ""}`} title={title} onClick={onClick}>
      <Icon className="h-4 w-4" />
    </Button>
  );
}

function VaultEditor({ editor, projects, action, onChange, onClose, onSubmit }) {
  function update(key, value) {
    onChange((current) => ({ ...current, [key]: value }));
  }
  function toggleSharedProject(projectID) {
    const selected = editor.shared_project_ids.map(Number);
    update("shared_project_ids", selected.includes(Number(projectID)) ? selected.filter((id) => id !== Number(projectID)) : [...selected, Number(projectID)]);
  }
  function updateUsageNote(index, key, value) {
    update("usage_notes", editor.usage_notes.map((note, noteIndex) => noteIndex === index ? { ...note, [key]: value } : note));
  }
  return (
    <Drawer open={editor.open} title={editor.mode === "edit" ? "Edit Vault item" : "Add Vault item"} description="Metadata is searchable. Never paste secret values into descriptions, tags, or usage notes." onClose={onClose} bodyClassName="overflow-y-auto">
      <form className="grid gap-5" onSubmit={onSubmit}>
        {editor.mode === "create" ? (
          <div className="grid grid-cols-2 rounded-md border border-stone-300 p-1">
            <Button type="button" variant={editor.source === "imported" ? "default" : "ghost"} className="h-9" onClick={() => update("source", "imported")}>Import value</Button>
            <Button type="button" variant={editor.source === "generated" ? "default" : "ghost"} className="h-9" onClick={() => update("source", "generated")}>Generate locally</Button>
          </div>
        ) : null}

        <Field>
          Environment name
          <Input autoFocus value={editor.name} onChange={(event) => update("name", event.target.value.toUpperCase())} placeholder="PROJECT_SERVICE_API_KEY" maxLength={128} required />
          <span className="text-xs font-normal text-stone-500">Use a specific uppercase name so AI clients can choose it reliably. Runtime-sensitive names such as PATH, LD_*, and BASH_FUNC_* are rejected.</span>
        </Field>

        {editor.mode === "create" && editor.source === "imported" ? (
          <Field>
            Secret value
            <Textarea className="min-h-28 font-mono" value={editor.value} onChange={(event) => update("value", event.target.value)} required />
          </Field>
        ) : null}
        {editor.mode === "create" && editor.source === "generated" ? (
          <Field>
            Generator
            <Select value={editor.generator_kind} onChange={(event) => update("generator_kind", event.target.value)}>
              {generatorKinds.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
            </Select>
          </Field>
        ) : null}

        <div className="grid gap-4 sm:grid-cols-2">
          <Field>
            Secret type
            <Select value={editor.secret_type} onChange={(event) => update("secret_type", event.target.value)}>
              {secretTypes.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
            </Select>
          </Field>
          <Field>
            Owner project
            <Select value={editor.owner_project_id} onChange={(event) => {
              update("owner_project_id", event.target.value);
              update("shared_project_ids", editor.shared_project_ids.filter((id) => Number(id) !== Number(event.target.value)));
            }} required>
              <option value="">Choose project</option>
              {projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}
            </Select>
          </Field>
        </div>

        <div className="grid gap-2">
          <p className="text-sm font-medium text-stone-800">Shared projects</p>
          <div className="grid gap-2 rounded-md border border-stone-200 p-3 sm:grid-cols-2">
            {projects.filter((project) => Number(project.id) !== Number(editor.owner_project_id)).map((project) => (
              <label key={project.id} className="flex items-center gap-2 text-sm">
                <Checkbox checked={editor.shared_project_ids.map(Number).includes(Number(project.id))} onChange={() => toggleSharedProject(project.id)} />
                <span className="truncate">{project.name}</span>
              </label>
            ))}
            {projects.length < 2 ? <p className="text-xs text-stone-500">Create another project to share this item.</p> : null}
          </div>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field>Provider<Input value={editor.provider} onChange={(event) => update("provider", event.target.value)} placeholder="GitHub" /></Field>
          <Field>Environment<Input value={editor.environment} onChange={(event) => update("environment", event.target.value)} placeholder="production" /></Field>
        </div>
        <Field>Description<Textarea value={editor.description} onChange={(event) => update("description", event.target.value)} placeholder="Non-secret purpose and access scope" /></Field>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field>Expires at<Input type="datetime-local" value={editor.expires_at} onChange={(event) => update("expires_at", event.target.value)} /></Field>
          <Field>Warning days<Input type="number" min="1" max="3650" value={editor.expiry_warning_days} onChange={(event) => update("expiry_warning_days", event.target.value)} required /></Field>
        </div>
        <Field>Tags<Input value={editor.tags} onChange={(event) => update("tags", event.target.value)} placeholder="deploy, production, ci" /></Field>

        <div className="grid gap-3">
          <div className="flex items-center justify-between gap-3">
            <div>
              <p className="text-sm font-medium text-stone-800">Used in</p>
              <p className="text-xs text-stone-500">Track places that need an update when this value changes.</p>
            </div>
            <Button type="button" variant="outline" className="h-9 px-3" onClick={() => update("usage_notes", [...editor.usage_notes, { location: "", notes: "" }])}>
              <Plus className="h-4 w-4" />
              Add
            </Button>
          </div>
          {editor.usage_notes.map((note, index) => (
            <div key={index} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_36px] gap-2">
              <Input value={note.location} onChange={(event) => updateUsageNote(index, "location", event.target.value)} placeholder="core-1: /opt/app/.env" />
              <Input value={note.notes} onChange={(event) => updateUsageNote(index, "notes", event.target.value)} placeholder="Optional note" />
              <Button type="button" variant="ghost" className="h-10 w-9 px-0" title="Remove usage note" onClick={() => update("usage_notes", editor.usage_notes.filter((_, noteIndex) => noteIndex !== index))}>
                <X className="h-4 w-4" />
              </Button>
            </div>
          ))}
        </div>

        {action.error && editor.open ? <Notice tone="bad">{action.error}</Notice> : null}
        <div className="grid gap-2 sm:grid-cols-2">
          <Button type="button" variant="outline" onClick={onClose}>Cancel</Button>
          <Button type="submit" disabled={!editor.name.trim() || !editor.owner_project_id || (editor.mode === "create" && editor.source === "imported" && !editor.value) || action.state === "saving"}>
            {action.state === "saving" ? "Saving..." : editor.mode === "edit" ? "Save metadata" : "Create Vault item"}
          </Button>
        </div>
      </form>
    </Drawer>
  );
}

function secretTypeLabel(value) {
  return secretTypes.find(([type]) => type === value)?.[1] || value;
}

function splitTags(value) {
  return String(value || "").split(",").map((tag) => tag.trim()).filter(Boolean);
}

function toRFC3339(value) {
  if (!value) return "";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? "" : parsed.toISOString();
}

function toLocalDateTime(value) {
  if (!value) return "";
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return "";
  const local = new Date(parsed.getTime() - parsed.getTimezoneOffset() * 60000);
  return local.toISOString().slice(0, 16);
}

function expiryState(item) {
  if (!item.expires_at) return { tone: "neutral", label: "Never" };
  const expiresAt = Date.parse(item.expires_at);
  const now = Date.now();
  if (expiresAt <= now) return { tone: "bad", label: "Expired" };
  const days = Math.max(1, Math.ceil((expiresAt - now) / 86400000));
  if (days <= Number(item.expiry_warning_days || 14)) return { tone: "warn", label: `${days}d left` };
  return { tone: "good", label: new Date(expiresAt).toLocaleDateString() };
}

function timeAgo(value) {
  const difference = Date.now() - Date.parse(value);
  if (!Number.isFinite(difference) || difference < 0) return "Just now";
  const minutes = Math.floor(difference / 60000);
  if (minutes < 1) return "Just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}
