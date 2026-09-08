import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { apiGet, apiPost, apiPut } from "../../lib/api";
import { toLocalDateTime, toRFC3339 } from "../../lib/date-time";
import { loadProjectOptions } from "../../lib/load-project-options";
import { useRequestGuard } from "../../lib/request-guard";

export const emptyVaultEditor = {
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

export function useVaultCollection() {
  const [items, setItems] = useState({ state: "loading", data: [], total: 0, error: null });
  const [projects, setProjects] = useState({ state: "loading", data: [], error: null });
  const [filters, setFilters] = useState({ project_id: "", query: "", expiry: "all" });
  const [editor, setEditor] = useState(emptyVaultEditor);
  const [action, setAction] = useState({ state: "idle", message: "", error: null });
  const filtersRef = useRef(filters);
  const searchTimer = useRef(null);
  const guard = useRequestGuard("vault-collection");
  const loadItems = useCallback(async () => {
    const request = guard.begin("items");
    setItems((current) => ({ ...current, state: "loading", error: null }));
    try {
      const params = new URLSearchParams();
      if (filters.project_id) params.set("project_id", filters.project_id);
      if (filters.query.trim()) params.set("q", filters.query.trim());
      const data = await apiGet(`/api/vault-items${params.size ? `?${params}` : ""}`, { signal: request.signal });
      if (request.isCurrent()) setItems({ state: "ready", data: data.items || [], total: data.total || 0, error: null });
    } catch (error) {
      if (request.isCurrent()) setItems({ state: "error", data: [], total: 0, error: error.message });
    } finally {
      request.complete();
    }
  }, [filters.project_id, filters.query, guard]);
  const loadProjects = useCallback(async () => {
    await loadProjectOptions(guard, setProjects);
  }, [guard]);
  const updateFilters = useCallback(
    (patch) => {
      const current = filtersRef.current;
      const next = { ...current, ...patch };
      if (next.project_id !== current.project_id || next.query !== current.query) guard.invalidate("items");
      filtersRef.current = next;
      setFilters(next);
    },
    [guard],
  );
  const visibleItems = useMemo(() => filterVaultItemsByExpiry(items.data, filters.expiry), [items.data, filters.expiry]);

  useEffect(() => {
    void loadProjects();
    return () => {
      window.clearTimeout(searchTimer.current);
    };
  }, [loadProjects]);

  useEffect(() => {
    window.clearTimeout(searchTimer.current);
    guard.invalidate("items");
    searchTimer.current = window.setTimeout(() => void loadItems(), 200);
    return () => window.clearTimeout(searchTimer.current);
  }, [guard, loadItems]);

  function openCreate() {
    guard.invalidate("editor-mutation");
    const owner = filters.project_id || projects.data.find((project) => project.slug !== "ungrouped")?.id || projects.data[0]?.id || "";
    setAction({ state: "idle", message: "", error: null });
    setEditor({ ...emptyVaultEditor, open: true, owner_project_id: owner });
  }

  function openEdit(item) {
    guard.invalidate("editor-mutation");
    setAction({ state: "idle", message: "", error: null });
    setEditor({
      ...emptyVaultEditor,
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
    guard.invalidate("editor-mutation");
    setEditor(emptyVaultEditor);
  }

  async function saveItem(event) {
    event.preventDefault();
    const snapshot = editor;
    const request = guard.begin("editor-mutation");
    setAction({ state: "saving", message: "", error: null });
    try {
      const common = vaultMetadataPayload(snapshot);
      if (snapshot.mode === "edit") {
        await apiPut(
          `/api/vault-items/${snapshot.item.id}`,
          { ...common, expected_metadata_revision: snapshot.item.metadata_revision },
          { signal: request.signal },
        );
      } else {
        await apiPost(
          "/api/vault-items",
          {
            ...common,
            source: snapshot.source,
            value: snapshot.source === "imported" ? snapshot.value : "",
            generator_kind: snapshot.source === "generated" ? snapshot.generator_kind : "",
          },
          { signal: request.signal },
        );
      }
      if (!request.isCurrent()) return;
      setEditor(emptyVaultEditor);
      setAction({ state: "ready", message: snapshot.mode === "edit" ? "Vault item updated." : "Vault item created.", error: null });
      await loadItems();
    } catch (error) {
      if (request.isCurrent()) setAction({ state: "error", message: "", error: error.message });
    } finally {
      request.complete();
    }
  }

  return {
    items,
    projects,
    filters,
    setFilters: updateFilters,
    visibleItems,
    editor,
    setEditor,
    action,
    setAction,
    loadItems,
    openCreate,
    openEdit,
    closeEditor,
    saveItem,
  };
}

export function filterVaultItemsByExpiry(items, expiry, now = Date.now()) {
  return items.filter((item) => {
    if (expiry === "expired") return item.expires_at && Date.parse(item.expires_at) <= now;
    if (expiry === "warning") {
      if (!item.expires_at) return false;
      const warningAt = Date.parse(item.expires_at) - Number(item.expiry_warning_days || 14) * 86400000;
      return Date.parse(item.expires_at) > now && warningAt <= now;
    }
    if (expiry === "none") return !item.expires_at;
    return true;
  });
}

function vaultMetadataPayload(editor) {
  return {
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
}

function splitTags(value) {
  return String(value || "")
    .split(",")
    .map((tag) => tag.trim())
    .filter(Boolean);
}
