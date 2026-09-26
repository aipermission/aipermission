import { ArchiveRestore, RotateCcw, Save, ShieldCheck } from "lucide-react";
import { useEffect, useEffectEvent, useLayoutEffect, useState } from "react";
import { apiGet, apiPost, apiPut } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import { useRequestGuard } from "../../lib/request-guard";
import { formatBytes } from "../../lib/file-transfer-utils";
import { Button } from "../ui/button";
import { Checkbox, Field, Input } from "../ui/form";
import { Notice } from "../ui/notice";
import { parseBackupKeepLatest } from "./backup-state";
import { backupStorageResponse, backupRetentionPolicyResponse, backupRetentionPreviewResponse, backupRetentionUpdateResponse, type BackupStorage, type BackupRetentionPolicy, type BackupRetentionPreview } from "./backup-retention-contracts";

const defaultKeepLatest = 10;

type RetentionForm = { enabled: boolean; keepLatest: string; applyNow: boolean };
type RetentionState = { status: "loading" | "ready" | "error"; storage: BackupStorage | null; policy: BackupRetentionPolicy | null; error: string };
type RetentionProps = { provider: { id: number }; onRecordsChanged?: () => void | Promise<void>; onBusyChange?: (_busy: boolean) => void };

export function BackupRetentionPanel({ provider, onRecordsChanged, onBusyChange }: RetentionProps) {
  const [state, setState] = useState<RetentionState>({ status: "loading", storage: null, policy: null, error: "" });
  const [form, setForm] = useState({ enabled: false, keepLatest: String(defaultKeepLatest), applyNow: true });
  const [preview, setPreview] = useState<BackupRetentionPreview | null>(null);
  const [action, setAction] = useState({ status: "idle", error: "", message: "" });
  const [formDirty, setFormDirty] = useState(false);
  const requestGuard = useRequestGuard(`backup-retention:${provider.id}`);
  const loadForEffect = useEffectEvent(() => loadRemoteState({ syncForm: true }));

  useLayoutEffect(() => {
    requestGuard.setScope(`backup-retention:${provider.id}`);
  }, [provider.id, requestGuard]);

  useEffect(() => {
    setPreview(null);
    setAction({ status: "idle", error: "", message: "" });
    setFormDirty(false);
    void loadForEffect();
  }, [provider.id]);

  const keepLatest = parseBackupKeepLatest(form.keepLatest);
  const previewMatches = preview?.keep_latest === keepLatest;
  const busy = action.status === "previewing" || action.status === "saving";

  useEffect(() => {
    onBusyChange?.(busy);
    return () => onBusyChange?.(false);
  }, [busy, onBusyChange]);

  function updateForm(patch: Partial<RetentionForm>) {
    if (busy) return;
    setForm((current) => ({ ...current, ...patch }));
    setPreview(null);
    setAction({ status: "idle", error: "", message: "" });
    setFormDirty(true);
  }

  async function loadRemoteState({ syncForm }: { syncForm: boolean }) {
    const request = requestGuard.begin("load");
    setState((current) => ({ ...current, status: "loading", error: "" }));
    try {
      const [storageValue, policyValue] = await Promise.all([
        apiGet(`/api/backup/providers/${provider.id}/storage`, { signal: request.signal }),
        apiGet(`/api/backup/providers/${provider.id}/retention`, { signal: request.signal }),
      ]);
      if (!request.isCurrent()) return;
      const storage = backupStorageResponse(storageValue);
      const policy = backupRetentionPolicyResponse(policyValue);
      setState({ status: "ready", storage, policy, error: "" });
      if (syncForm) {
        setForm({ enabled: Boolean(policy.enabled), keepLatest: String(policy.keep_latest || defaultKeepLatest), applyNow: true });
        setFormDirty(false);
      }
    } catch (error) {
      if (!request.isCurrent()) return;
      setState({ status: "error", storage: null, policy: null, error: errorMessage(error, "Unable to read backup retention status.") });
    } finally {
      request.complete();
    }
  }

  async function refresh() {
    if (busy) return;
    await loadRemoteState({ syncForm: !formDirty });
  }

  async function requestPreview() {
    if (keepLatest === null || busy) return;
    const request = requestGuard.begin("mutation");
    setAction({ status: "previewing", error: "", message: "" });
    try {
      const result = backupRetentionPreviewResponse(await apiPost(`/api/backup/providers/${provider.id}/retention/preview`, { keep_latest: keepLatest }));
      if (!request.isCurrent()) return;
      setPreview(result);
      setAction({ status: "idle", error: "", message: "" });
    } catch (error) {
      if (!request.isCurrent()) return;
      setPreview(null);
      setAction({ status: "error", error: errorMessage(error, "Unable to preview backup retention."), message: "" });
    } finally {
      request.complete();
    }
  }

  async function savePolicy() {
    if (busy || (form.enabled && (keepLatest === null || !previewMatches))) return;
    const request = requestGuard.begin("mutation");
    setAction({ status: "saving", error: "", message: "" });
    try {
      const result = backupRetentionUpdateResponse(await apiPut(`/api/backup/providers/${provider.id}/retention`, {
        enabled: form.enabled,
        keep_latest: form.enabled ? keepLatest : 0,
        apply_now: form.enabled && form.applyNow,
      }));
      if (!request.isCurrent()) return;
      const deletedCount = Number(result.deleted_count || 0);
      setState((current) => ({ ...current, policy: result.policy }));
      setPreview(result.preview || null);
      await loadRemoteState({ syncForm: true });
      if (!request.isCurrent()) return;
      if (deletedCount > 0) await onRecordsChanged?.();
      if (!request.isCurrent()) return;
      setAction({
        status: "idle",
        error: "",
        message: form.enabled
          ? `Automatic retention enabled. ${deletedCount} existing backup${deletedCount === 1 ? "" : "s"} removed.`
          : "Automatic retention disabled.",
      });
    } catch (error) {
      if (!request.isCurrent()) return;
      setAction({ status: "error", error: errorMessage(error, "Unable to save backup retention."), message: "" });
    } finally {
      request.complete();
    }
  }

  return (
    <section className="grid gap-3 rounded-md border border-stone-200 p-3" aria-label="Backup retention and storage">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <p className="text-sm font-semibold text-stone-950">Storage and automatic retention</p>
          <p className="mt-1 text-xs text-stone-500">Remote service limits and this database stream's cleanup policy.</p>
        </div>
        <Button
          type="button"
          variant="outline"
          className="h-8 w-8 px-0"
          title="Refresh storage and retention"
          onClick={refresh}
          disabled={state.status === "loading" || busy}
        >
          <RotateCcw className={`h-4 w-4 ${state.status === "loading" ? "animate-spin" : ""}`} />
        </Button>
      </div>

      {state.status === "error" ? (
        <Notice tone="bad">Could not read backup storage or retention status: {state.error}</Notice>
      ) : state.status === "loading" ? (
        <p className="text-sm text-stone-500">Loading remote storage and retention status...</p>
      ) : (
        <>
          {state.storage ? <StorageSummary storage={state.storage} /> : null}
          <div className="grid gap-3 border-t border-stone-200 pt-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-end">
            <div className="grid gap-3 sm:grid-cols-[auto_minmax(120px,180px)_auto] sm:items-end">
              <label className="flex h-10 items-center gap-2 text-sm font-medium text-stone-800">
                <Checkbox checked={form.enabled} disabled={busy} onChange={(event) => updateForm({ enabled: event.target.checked })} />
                Automatic retention
              </label>
              <Field>
                Keep latest
                <Input
                  type="number"
                  min="1"
                  max="1000"
                  value={form.keepLatest}
                  disabled={!form.enabled || busy}
                  onChange={(event) => updateForm({ keepLatest: event.target.value })}
                />
              </Field>
              <label className="flex h-10 items-center gap-2 text-sm text-stone-700">
                <Checkbox
                  checked={form.applyNow}
                  disabled={!form.enabled || busy}
                  onChange={(event) => updateForm({ applyNow: event.target.checked })}
                />
                Apply now
              </label>
            </div>
            <div className="flex justify-end gap-2">
              {form.enabled ? (
                <Button
                  type="button"
                  variant="outline"
                  onClick={requestPreview}
                  disabled={keepLatest === null || action.status === "previewing" || action.status === "saving"}
                >
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
          {previewMatches && preview ? <RetentionPreview preview={preview} applyNow={form.applyNow} /> : null}
        </>
      )}
      {action.message ? <Notice tone="good">{action.message}</Notice> : null}
      {action.error ? <Notice tone="bad">{action.error}</Notice> : null}
    </section>
  );
}

function StorageSummary({ storage }: { storage: BackupStorage }) {
  return (
    <div className="grid gap-2 sm:grid-cols-3">
      <Metric label="Used" value={formatBytes(storage.used_bytes)} />
      <Metric label="Quota" value={storage.quota_enabled ? formatBytes(storage.quota_bytes ?? 0) : "Not configured"} />
      <Metric label="Remaining" value={storage.quota_enabled ? formatBytes(storage.remaining_bytes ?? 0) : "Unlimited by service"} />
      {storage.pending_deletions > 0 ? (
        <p className="sm:col-span-3 text-xs text-amber-700">
          {storage.pending_deletions} remote file deletion{storage.pending_deletions === 1 ? " is" : "s are"} pending retry.
        </p>
      ) : null}
    </div>
  );
}

function RetentionPreview({ preview, applyNow }: { preview: BackupRetentionPreview; applyNow: boolean }) {
  return (
    <Notice tone={preview.delete_count > 0 && applyNow ? "warn" : "good"}>
      <span className="inline-flex items-center gap-2 font-medium">
        <ShieldCheck className="h-4 w-4" />
        The newest recovery version is always protected.
      </span>{" "}
      Keep {preview.retain_count} ({formatBytes(preview.retain_bytes)}); {applyNow ? "delete" : "would delete"} {preview.delete_count} (
      {formatBytes(preview.delete_bytes)}).
    </Notice>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-stone-200 px-3 py-2">
      <p className="text-[11px] font-semibold uppercase text-stone-500">{label}</p>
      <p className="mt-1 text-sm font-medium text-stone-900">{value}</p>
    </div>
  );
}
