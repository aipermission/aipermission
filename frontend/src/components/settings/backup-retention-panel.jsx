import { ArchiveRestore, RotateCcw, Save, ShieldCheck } from "lucide-react";
import { useEffect, useState } from "react";
import { apiGet, apiPost, apiPut } from "../../lib/api";
import { formatBytes } from "../../lib/file-transfer-utils";
import { Button } from "../ui/button";
import { Checkbox, Field, Input } from "../ui/form";
import { Notice } from "../ui/notice";

const defaultKeepLatest = 10;

export function BackupRetentionPanel({ provider, onRecordsChanged }) {
  const [state, setState] = useState({ status: "loading", storage: null, policy: null, error: "" });
  const [form, setForm] = useState({ enabled: false, keepLatest: String(defaultKeepLatest), applyNow: true });
  const [preview, setPreview] = useState(null);
  const [action, setAction] = useState({ status: "idle", error: "", message: "" });

  useEffect(() => {
    let active = true;
    setState({ status: "loading", storage: null, policy: null, error: "" });
    setPreview(null);
    setAction({ status: "idle", error: "", message: "" });
    Promise.all([
      apiGet(`/api/backup/providers/${provider.id}/storage`),
      apiGet(`/api/backup/providers/${provider.id}/retention`),
    ])
      .then(([storage, policy]) => {
        if (!active) return;
        setState({ status: "ready", storage, policy, error: "" });
        setForm({
          enabled: Boolean(policy.enabled),
          keepLatest: String(policy.keep_latest || defaultKeepLatest),
          applyNow: true,
        });
      })
      .catch((error) => {
        if (active) setState({ status: "error", storage: null, policy: null, error: error.message });
      });
    return () => {
      active = false;
    };
  }, [provider.id]);

  const keepLatest = parseKeepLatest(form.keepLatest);
  const previewMatches = preview?.keep_latest === keepLatest;

  function updateForm(patch) {
    setForm((current) => ({ ...current, ...patch }));
    setPreview(null);
    setAction({ status: "idle", error: "", message: "" });
  }

  async function refresh() {
    setState((current) => ({ ...current, status: "loading", error: "" }));
    try {
      const [storage, policy] = await Promise.all([
        apiGet(`/api/backup/providers/${provider.id}/storage`),
        apiGet(`/api/backup/providers/${provider.id}/retention`),
      ]);
      setState({ status: "ready", storage, policy, error: "" });
      setForm((current) => ({
        ...current,
        enabled: Boolean(policy.enabled),
        keepLatest: String(policy.keep_latest || parseKeepLatest(current.keepLatest) || defaultKeepLatest),
      }));
    } catch (error) {
      setState({ status: "error", storage: null, policy: null, error: error.message });
    }
  }

  async function requestPreview() {
    if (keepLatest === null) return;
    setAction({ status: "previewing", error: "", message: "" });
    try {
      const result = await apiPost(`/api/backup/providers/${provider.id}/retention/preview`, { keep_latest: keepLatest });
      setPreview(result);
      setAction({ status: "idle", error: "", message: "" });
    } catch (error) {
      setPreview(null);
      setAction({ status: "error", error: error.message, message: "" });
    }
  }

  async function savePolicy() {
    if (form.enabled && (keepLatest === null || !previewMatches)) return;
    setAction({ status: "saving", error: "", message: "" });
    try {
      const result = await apiPut(`/api/backup/providers/${provider.id}/retention`, {
        enabled: form.enabled,
        keep_latest: form.enabled ? keepLatest : 0,
        apply_now: form.enabled && form.applyNow,
      });
      const deletedCount = Number(result.deleted_count || 0);
      setState((current) => ({ ...current, policy: result.policy }));
      setPreview(result.preview || null);
      setAction({
        status: "idle",
        error: "",
        message: form.enabled
          ? `Automatic retention enabled. ${deletedCount} existing backup${deletedCount === 1 ? "" : "s"} removed.`
          : "Automatic retention disabled.",
      });
      await refresh();
      if (deletedCount > 0) await onRecordsChanged?.();
    } catch (error) {
      setAction({ status: "error", error: error.message, message: "" });
    }
  }

  return (
    <section className="grid gap-3 rounded-md border border-stone-200 p-3" aria-label="Backup retention and storage">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <p className="text-sm font-semibold text-stone-950">Storage and automatic retention</p>
          <p className="mt-1 text-xs text-stone-500">Remote service limits and this database stream's cleanup policy.</p>
        </div>
        <Button type="button" variant="outline" className="h-8 w-8 px-0" title="Refresh storage and retention" onClick={refresh} disabled={state.status === "loading"}>
          <RotateCcw className={`h-4 w-4 ${state.status === "loading" ? "animate-spin" : ""}`} />
        </Button>
      </div>

      {state.status === "error" ? (
        <Notice tone="bad">Could not read backup storage or retention status: {state.error}</Notice>
      ) : state.status === "loading" ? (
        <p className="text-sm text-stone-500">Loading remote storage and retention status...</p>
      ) : (
        <>
          <StorageSummary storage={state.storage} />
          <div className="grid gap-3 border-t border-stone-200 pt-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-end">
            <div className="grid gap-3 sm:grid-cols-[auto_minmax(120px,180px)_auto] sm:items-end">
              <label className="flex h-10 items-center gap-2 text-sm font-medium text-stone-800">
                <Checkbox checked={form.enabled} onChange={(event) => updateForm({ enabled: event.target.checked })} />
                Automatic retention
              </label>
              <Field>
                Keep latest
                <Input
                  type="number"
                  min="1"
                  max="1000"
                  value={form.keepLatest}
                  disabled={!form.enabled}
                  onChange={(event) => updateForm({ keepLatest: event.target.value })}
                />
              </Field>
              <label className="flex h-10 items-center gap-2 text-sm text-stone-700">
                <Checkbox
                  checked={form.applyNow}
                  disabled={!form.enabled}
                  onChange={(event) => updateForm({ applyNow: event.target.checked })}
                />
                Apply now
              </label>
            </div>
            <div className="flex justify-end gap-2">
              {form.enabled ? (
                <Button type="button" variant="outline" onClick={requestPreview} disabled={keepLatest === null || action.status === "previewing" || action.status === "saving"}>
                  <ArchiveRestore className="h-4 w-4" />
                  {action.status === "previewing" ? "Previewing..." : "Preview"}
                </Button>
              ) : null}
              <Button type="button" onClick={savePolicy} disabled={action.status === "saving" || (form.enabled && !previewMatches)}>
                <Save className="h-4 w-4" />
                {action.status === "saving" ? "Saving..." : "Save policy"}
              </Button>
            </div>
          </div>
          {form.enabled && !previewMatches ? (
            <p className="text-xs text-amber-700">Preview the current retention count before saving.</p>
          ) : null}
          {previewMatches ? <RetentionPreview preview={preview} applyNow={form.applyNow} /> : null}
        </>
      )}
      {action.message ? <Notice tone="good">{action.message}</Notice> : null}
      {action.error ? <Notice tone="bad">{action.error}</Notice> : null}
    </section>
  );
}

function StorageSummary({ storage }) {
  return (
    <div className="grid gap-2 sm:grid-cols-3">
      <Metric label="Used" value={formatBytes(storage.used_bytes)} />
      <Metric label="Quota" value={storage.quota_enabled ? formatBytes(storage.quota_bytes) : "Not configured"} />
      <Metric label="Remaining" value={storage.quota_enabled ? formatBytes(storage.remaining_bytes) : "Unlimited by service"} />
      {storage.pending_deletions > 0 ? (
        <p className="sm:col-span-3 text-xs text-amber-700">{storage.pending_deletions} remote file deletion{storage.pending_deletions === 1 ? " is" : "s are"} pending retry.</p>
      ) : null}
    </div>
  );
}

function RetentionPreview({ preview, applyNow }) {
  return (
    <Notice tone={preview.delete_count > 0 && applyNow ? "warn" : "good"}>
      <span className="inline-flex items-center gap-2 font-medium"><ShieldCheck className="h-4 w-4" />The newest recovery version is always protected.</span>{" "}
      Keep {preview.retain_count} ({formatBytes(preview.retain_bytes)}); {applyNow ? "delete" : "would delete"} {preview.delete_count} ({formatBytes(preview.delete_bytes)}).
    </Notice>
  );
}

function Metric({ label, value }) {
  return (
    <div className="rounded-md border border-stone-200 px-3 py-2">
      <p className="text-[11px] font-semibold uppercase text-stone-500">{label}</p>
      <p className="mt-1 text-sm font-medium text-stone-900">{value}</p>
    </div>
  );
}

function parseKeepLatest(value) {
  const parsed = Number.parseInt(String(value), 10);
  return Number.isInteger(parsed) && parsed >= 1 && parsed <= 1000 ? parsed : null;
}
