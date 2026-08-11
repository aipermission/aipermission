import { Edit3, Eye, Link2, Plus, RotateCw, Trash2, X } from "lucide-react";
import { useEffect, useMemo } from "react";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { DateTimePicker } from "../components/ui/date-time-picker";
import { Dialog } from "../components/ui/dialog";
import { Drawer } from "../components/ui/drawer";
import { Checkbox, Field, Input, Select, Textarea } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { formatRelativeAge } from "../lib/date-time";

const secretTypes = [
  ["generic_secret", "Generic secret"],
  ["api_key", "API key"],
  ["access_token", "Access token"],
  ["password", "Password"],
  ["client_secret", "Client secret"],
  ["webhook_hmac", "Webhook / HMAC secret"],
  ["connection", "Connection string"],
];

export const generatorKinds = [
  ["random_token", "Random token (32 bytes)"],
  ["hex_secret", "Hex secret (32 bytes)"],
  ["password", "Password (32 characters)"],
  ["long_hmac_secret", "Long HMAC secret (64 bytes)"],
  ["uuid_v4", "UUID v4 (identifier)"],
];

export function VaultRow({ item, projects, onEdit, onReveal, onReplace, onBindings, onDelete }) {
  const projectNames = [
    item.owner_project_name,
    ...(item.project_ids || []).map((id) => projects.find((project) => Number(project.id) === Number(id))?.name).filter(Boolean),
  ];
  const visibleTags = (item.tags || []).slice(0, 3);
  const hiddenTagCount = Math.max(0, (item.tags || []).length - visibleTags.length);
  const expiry = expiryState(item);
  return (
    <tr className="hover:bg-stone-50">
      <td className="px-4 py-3">
        <p className="truncate font-mono text-xs font-semibold text-stone-950">{item.name}</p>
        <p className="mt-1 truncate text-xs text-stone-500">
          {[item.provider, item.environment].filter(Boolean).join(" / ") || "Local secret"}
        </p>
        {visibleTags.length > 0 ? (
          <div className="mt-2 flex min-w-0 flex-wrap gap-1" title={(item.tags || []).join(", ")}>
            {visibleTags.map((tag) => (
              <Badge key={tag} tone="neutral" className="max-w-32 truncate px-2 py-0.5 font-medium">
                {tag}
              </Badge>
            ))}
            {hiddenTagCount > 0 ? (
              <Badge tone="neutral" className="px-2 py-0.5 font-medium">
                +{hiddenTagCount}
              </Badge>
            ) : null}
          </div>
        ) : null}
      </td>
      <td className="px-4 py-3">
        <Badge tone="neutral">{secretTypeLabel(item.secret_type)}</Badge>
      </td>
      <td className="px-4 py-3">
        <div className="flex flex-wrap gap-1">
          {projectNames.map((name, index) => (
            <Badge key={`${name}-${index}`} tone={index === 0 ? "good" : "neutral"}>
              {name}
            </Badge>
          ))}
        </div>
      </td>
      <td className="px-4 py-3">
        <Badge tone={expiry.tone}>{expiry.label}</Badge>
      </td>
      <td className="px-4 py-3 text-xs text-stone-500">{item.last_used_at ? formatRelativeAge(item.last_used_at) : "Never"}</td>
      <td className="px-4 py-3">
        <div className="flex justify-end gap-2">
          <IconButton title="Default session bindings" icon={Link2} onClick={onBindings} />
          <IconButton title="Reveal and copy" icon={Eye} onClick={onReveal} />
          <IconButton title="Replace local value" icon={RotateCw} onClick={onReplace} />
          <IconButton title="Edit metadata" icon={Edit3} onClick={onEdit} />
          <IconButton title="Delete" icon={Trash2} onClick={onDelete} />
        </div>
      </td>
    </tr>
  );
}

export function VaultBindingsDialog({ state, projects, onChange, onClose, onSave, onDelete }) {
  const allowedProjects = useMemo(() => {
    if (!state.item) return [];
    const ids = new Set([Number(state.item.owner_project_id), ...(state.item.project_ids || []).map(Number)]);
    return projects.filter((project) => ids.has(Number(project.id)));
  }, [state.item, projects]);
  const selectedTarget = state.targets.find((target) => String(target.id) === String(state.target_id));
  const current = selectedBinding(state);

  useEffect(() => {
    if (!state.open) return;
    const nextReplace = current?.replace_existing || false;
    if (state.replace_existing !== nextReplace) {
      onChange((value) => ({ ...value, replace_existing: nextReplace }));
    }
  }, [
    state.open,
    state.source_project_id,
    state.target_id,
    state.profile_id,
    state.replace_existing,
    current?.id,
    current?.replace_existing,
    onChange,
  ]);

  function update(key, value) {
    onChange((currentState) => ({ ...currentState, [key]: value, error: null }));
  }

  return (
    <Dialog
      open={state.open}
      title={`Default environment for ${state.item?.name || "Vault item"}`}
      description="Bindings preselect this item for future sessions. They do not grant an AI permission."
      onClose={onClose}
      size="xl"
    >
      <div className="grid gap-4">
        <form className="grid gap-3 rounded-lg border border-stone-200 p-4" onSubmit={onSave}>
          <div className="grid gap-3 md:grid-cols-3">
            <Field>
              Source project
              <Select value={state.source_project_id} onChange={(event) => update("source_project_id", event.target.value)} required>
                {allowedProjects.map((project) => (
                  <option key={project.id} value={project.id}>
                    {project.name}
                  </option>
                ))}
              </Select>
            </Field>
            <Field>
              Connector target
              <Select
                value={state.target_id}
                onChange={(event) =>
                  onChange((value) => ({ ...value, target_id: event.target.value, profile_id: "", replace_existing: false, error: null }))
                }
                required
              >
                <option value="">Choose target</option>
                {state.targets.map((target) => (
                  <option key={target.id} value={target.id}>
                    {target.name} ({target.connector_kind})
                  </option>
                ))}
              </Select>
            </Field>
            <Field>
              Credential profile
              <Select
                value={state.profile_id}
                onChange={(event) => update("profile_id", event.target.value)}
                disabled={!selectedTarget}
                required
              >
                <option value="">Choose profile</option>
                {(selectedTarget?.profiles || []).map((profile) => (
                  <option key={profile.id} value={profile.id}>
                    {profile.label}
                  </option>
                ))}
              </Select>
            </Field>
          </div>
          <label className="flex items-center gap-2 text-sm text-stone-700">
            <Checkbox checked={state.replace_existing} onChange={(event) => update("replace_existing", event.target.checked)} />
            Overwrite an existing shell value with the same name
          </label>
          {state.error ? <Notice tone="bad">{state.error}</Notice> : null}
          <div className="flex justify-end">
            <Button type="submit" disabled={!state.target_id || !state.profile_id || state.state === "saving"}>
              <Link2 className="h-4 w-4" />
              {state.state === "saving" ? "Saving..." : current ? "Update binding" : "Add binding"}
            </Button>
          </div>
        </form>

        <div className="max-h-64 overflow-y-auto rounded-lg border border-stone-200">
          {state.data.map((item) => (
            <div key={item.id} className="flex items-center justify-between gap-3 border-b border-stone-200 px-4 py-3 last:border-b-0">
              <div className="min-w-0">
                <p className="truncate text-sm font-semibold">
                  {item.target_name} / {item.profile_label}
                </p>
                <p className="truncate text-xs text-stone-500">
                  {item.source_project_name} · {item.connector_kind} ·{" "}
                  {item.replace_existing ? "overwrite shell value" : "keep shell value"}
                </p>
              </div>
              <IconButton title="Remove binding" icon={Trash2} onClick={() => onDelete(item)} />
            </div>
          ))}
          {state.state === "loading" ? (
            <div className="p-4">
              <Notice>Loading default bindings...</Notice>
            </div>
          ) : null}
          {state.state !== "loading" && state.data.length === 0 ? (
            <p className="p-4 text-sm text-stone-500">No default bindings yet.</p>
          ) : null}
        </div>
      </div>
    </Dialog>
  );
}

export function selectedBinding(state) {
  return (
    state.data.find(
      (item) =>
        Number(item.source_project_id) === Number(state.source_project_id) &&
        Number(item.target_id) === Number(state.target_id) &&
        Number(item.profile_id) === Number(state.profile_id),
    ) || null
  );
}

function IconButton({ title, icon: Icon, onClick }) {
  return (
    <Button type="button" variant="outline" className="h-9 w-9 px-0" title={title} onClick={onClick}>
      <Icon className="h-4 w-4" />
    </Button>
  );
}

export function VaultEditor({ editor, projects, action, onChange, onClose, onSubmit }) {
  function update(key, value) {
    onChange((current) => ({ ...current, [key]: value }));
  }
  function toggleSharedProject(projectID) {
    const selected = editor.shared_project_ids.map(Number);
    update(
      "shared_project_ids",
      selected.includes(Number(projectID)) ? selected.filter((id) => id !== Number(projectID)) : [...selected, Number(projectID)],
    );
  }
  function updateUsageNote(index, key, value) {
    update(
      "usage_notes",
      editor.usage_notes.map((note, noteIndex) => (noteIndex === index ? { ...note, [key]: value } : note)),
    );
  }
  return (
    <Drawer
      open={editor.open}
      title={editor.mode === "edit" ? "Edit Vault item" : "Add Vault item"}
      description="Metadata is searchable. Never paste secret values into descriptions, tags, or usage notes."
      onClose={onClose}
      bodyClassName="overflow-y-auto"
    >
      <form className="grid gap-5" onSubmit={onSubmit}>
        {editor.mode === "create" ? (
          <div className="grid grid-cols-2 rounded-md border border-stone-300 p-1">
            <Button
              type="button"
              variant={editor.source === "imported" ? "default" : "ghost"}
              className="h-9"
              onClick={() => update("source", "imported")}
            >
              Import value
            </Button>
            <Button
              type="button"
              variant={editor.source === "generated" ? "default" : "ghost"}
              className="h-9"
              onClick={() => update("source", "generated")}
            >
              Generate locally
            </Button>
          </div>
        ) : null}

        <Field>
          Environment name
          <Input
            autoFocus
            value={editor.name}
            onChange={(event) => update("name", event.target.value.toUpperCase())}
            placeholder="PROJECT_SERVICE_API_KEY"
            maxLength={128}
            required
          />
          <span className="text-xs font-normal text-stone-500">
            Use a specific uppercase name so AI clients can choose it reliably. Runtime-sensitive names such as PATH, LD_*, and BASH_FUNC_*
            are rejected.
          </span>
        </Field>

        {editor.mode === "create" && editor.source === "imported" ? (
          <Field>
            Secret value
            <Textarea
              className="min-h-28 font-mono"
              value={editor.value}
              onChange={(event) => update("value", event.target.value)}
              required
            />
          </Field>
        ) : null}
        {editor.mode === "create" && editor.source === "generated" ? (
          <Field>
            Generator
            <Select value={editor.generator_kind} onChange={(event) => update("generator_kind", event.target.value)}>
              {generatorKinds.map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </Select>
          </Field>
        ) : null}

        <div className="grid gap-4 sm:grid-cols-2">
          <Field>
            Secret type
            <Select value={editor.secret_type} onChange={(event) => update("secret_type", event.target.value)}>
              {secretTypes.map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </Select>
          </Field>
          <Field>
            Owner project
            <Select
              value={editor.owner_project_id}
              onChange={(event) => {
                update("owner_project_id", event.target.value);
                update(
                  "shared_project_ids",
                  editor.shared_project_ids.filter((id) => Number(id) !== Number(event.target.value)),
                );
              }}
              required
            >
              <option value="">Choose project</option>
              {projects.map((project) => (
                <option key={project.id} value={project.id}>
                  {project.name}
                </option>
              ))}
            </Select>
          </Field>
        </div>

        <div className="grid gap-2">
          <p className="text-sm font-medium text-stone-800">Shared projects</p>
          <div className="grid gap-2 rounded-md border border-stone-200 p-3 sm:grid-cols-2">
            {projects
              .filter((project) => Number(project.id) !== Number(editor.owner_project_id))
              .map((project) => (
                <label key={project.id} className="flex items-center gap-2 text-sm">
                  <Checkbox
                    checked={editor.shared_project_ids.map(Number).includes(Number(project.id))}
                    onChange={() => toggleSharedProject(project.id)}
                  />
                  <span className="truncate">{project.name}</span>
                </label>
              ))}
            {projects.length < 2 ? <p className="text-xs text-stone-500">Create another project to share this item.</p> : null}
          </div>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field>
            Provider
            <Input value={editor.provider} onChange={(event) => update("provider", event.target.value)} placeholder="GitHub" />
          </Field>
          <Field>
            Environment
            <Input value={editor.environment} onChange={(event) => update("environment", event.target.value)} placeholder="production" />
          </Field>
        </div>
        <Field>
          Description
          <Textarea
            value={editor.description}
            onChange={(event) => update("description", event.target.value)}
            placeholder="Non-secret purpose and access scope"
          />
        </Field>

        <div className="grid gap-4 sm:grid-cols-[minmax(0,3fr)_minmax(120px,1fr)]">
          <Field>
            Expires at
            <DateTimePicker value={editor.expires_at} onChange={(value) => update("expires_at", value)} />
          </Field>
          <Field>
            Warning days
            <Input
              type="number"
              min="1"
              max="3650"
              value={editor.expiry_warning_days}
              onChange={(event) => update("expiry_warning_days", event.target.value)}
              required
            />
          </Field>
        </div>
        <Field>
          Tags
          <Input value={editor.tags} onChange={(event) => update("tags", event.target.value)} placeholder="deploy, production, ci" />
        </Field>

        <div className="grid gap-3">
          <div className="flex items-center justify-between gap-3">
            <div>
              <p className="text-sm font-medium text-stone-800">Used in</p>
              <p className="text-xs text-stone-500">Track places that need an update when this value changes.</p>
            </div>
            <Button
              type="button"
              variant="outline"
              className="h-9 px-3"
              onClick={() => update("usage_notes", [...editor.usage_notes, { location: "", notes: "" }])}
            >
              <Plus className="h-4 w-4" />
              Add
            </Button>
          </div>
          {editor.usage_notes.map((note, index) => (
            <div key={index} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_36px] gap-2">
              <Input
                value={note.location}
                onChange={(event) => updateUsageNote(index, "location", event.target.value)}
                placeholder="core-1: /opt/app/.env"
              />
              <Input
                value={note.notes}
                onChange={(event) => updateUsageNote(index, "notes", event.target.value)}
                placeholder="Optional note"
              />
              <Button
                type="button"
                variant="ghost"
                className="h-10 w-9 px-0"
                title="Remove usage note"
                onClick={() =>
                  update(
                    "usage_notes",
                    editor.usage_notes.filter((_, noteIndex) => noteIndex !== index),
                  )
                }
              >
                <X className="h-4 w-4" />
              </Button>
            </div>
          ))}
        </div>

        {action.error && editor.open ? <Notice tone="bad">{action.error}</Notice> : null}
        <div className="grid gap-2 sm:grid-cols-2">
          <Button type="button" variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button
            type="submit"
            disabled={
              !editor.name.trim() ||
              !editor.owner_project_id ||
              (editor.mode === "create" && editor.source === "imported" && !editor.value) ||
              action.state === "saving"
            }
          >
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

function expiryState(item) {
  if (!item.expires_at) return { tone: "neutral", label: "Never" };
  const expiresAt = Date.parse(item.expires_at);
  const now = Date.now();
  if (expiresAt <= now) return { tone: "bad", label: "Expired" };
  const days = Math.max(1, Math.ceil((expiresAt - now) / 86400000));
  if (days <= Number(item.expiry_warning_days || 14)) return { tone: "warn", label: `${days}d left` };
  return { tone: "good", label: new Date(expiresAt).toLocaleDateString() };
}
