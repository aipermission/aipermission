import { History, RotateCcw, Trash2 } from "lucide-react";
import { useEffect, useEffectEvent, useState } from "react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { Dialog } from "../../../components/ui/dialog";
import { Notice } from "../../../components/ui/notice";
import { formatBytes } from "../../../lib/file-transfer-utils";

export function S3VersionsDialog({ open, objectKey, theme, borderClass, mutedClass, onClose, onRun, onChanged }) {
  const [versions, setVersions] = useState([]);
  const [nextCursor, setNextCursor] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [confirmation, setConfirmation] = useState(null);
  const loadForEffect = useEffectEvent(() => loadVersions({ reset: true }));

  useEffect(() => {
    if (!open || !objectKey) return;
    setVersions([]);
    setNextCursor("");
    setError("");
    setConfirmation(null);
    void loadForEffect();
  }, [open, objectKey]);

  if (!open) return null;

  async function loadVersions({ reset, cursor = "" }) {
    setPending(true);
    setError("");
    try {
      const item = await onRun({
        actionName: "list_object_versions",
        input: { key: objectKey, cursor, limit: 100 },
        reason: "manual S3 object version list",
        busy: "reading versions",
      });
      if (!item) return;
      const nextVersions = Array.isArray(item.output?.versions) ? item.output.versions : [];
      setVersions((current) => (reset ? nextVersions : [...current, ...nextVersions]));
      setNextCursor(item.output?.next_cursor || "");
    } catch (loadError) {
      setError(loadError.message || "Object versions could not be loaded.");
    } finally {
      setPending(false);
    }
  }

  async function confirmAction() {
    if (!confirmation || pending) return;
    setPending(true);
    setError("");
    try {
      const item = await onRun({
        actionName: confirmation.action,
        input: { key: objectKey, version_id: confirmation.version.version_id },
        reason: `manual S3 object version ${confirmation.action === "restore_object_version" ? "restore" : "delete"}`,
        busy: confirmation.action === "restore_object_version" ? "restoring version" : "deleting version",
      });
      if (!item) return;
      setConfirmation(null);
      await loadVersions({ reset: true });
      await onChanged?.();
    } catch (actionError) {
      setError(actionError.message || "Object version action failed.");
    } finally {
      setPending(false);
    }
  }

  const detailClass = theme === "light" ? "border-stone-200 bg-stone-50" : "border-stone-700 bg-stone-900";
  return (
    <>
      <Dialog
        open={open}
        onClose={onClose}
        title="S3 object versions"
        description={objectKey}
        size="wide"
        closeDisabled={pending}
        closeOnOverlay={false}
      >
        <div className="grid max-h-[calc(100vh-180px)] min-h-80 grid-rows-[auto_minmax(0,1fr)_auto] gap-3">
          <Notice tone="warn">Restoring creates a new current version. Deleting a version or delete marker is permanent.</Notice>
          <div className={`min-h-0 overflow-auto rounded-lg border ${borderClass}`}>
            {versions.length === 0 ? (
              <div className="grid min-h-56 place-items-center p-6 text-center">
                <p className={`text-sm ${mutedClass}`}>{pending ? "Reading object versions..." : "No stored versions were returned."}</p>
              </div>
            ) : (
              <div className="divide-y divide-stone-200 dark:divide-stone-700">
                {versions.map((version) => (
                  <div className="flex items-center justify-between gap-4 p-3" key={`${version.version_id}-${version.delete_marker}`}>
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <p className="truncate font-mono text-xs" title={version.version_id}>
                          {version.version_id}
                        </p>
                        {version.is_latest ? <Badge tone="good">current</Badge> : null}
                        {version.delete_marker ? <Badge tone="bad">delete marker</Badge> : null}
                      </div>
                      <p className={`mt-1 text-xs ${mutedClass}`}>
                        {version.last_modified || "unknown time"} · {version.delete_marker ? "marker" : formatBytes(version.size)}
                      </p>
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                      <Button
                        type="button"
                        variant="outline"
                        className="h-8 w-8 px-0"
                        title="Restore this version"
                        disabled={pending || version.delete_marker}
                        onClick={() => setConfirmation({ action: "restore_object_version", version })}
                      >
                        <RotateCcw className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        type="button"
                        variant="danger"
                        className="h-8 w-8 px-0"
                        title="Delete this version"
                        disabled={pending}
                        onClick={() => setConfirmation({ action: "delete_object_version", version })}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
          <div className="grid gap-3">
            {error ? <Notice tone="bad">{error}</Notice> : null}
            <div className="flex justify-between gap-2">
              <Button
                type="button"
                variant="outline"
                className="h-8"
                disabled={!nextCursor || pending}
                onClick={() => loadVersions({ reset: false, cursor: nextCursor })}
              >
                Load more
              </Button>
              <Button type="button" variant="outline" onClick={onClose} disabled={pending}>
                Close
              </Button>
            </div>
          </div>
        </div>
      </Dialog>
      <Dialog
        open={Boolean(confirmation)}
        onClose={() => !pending && setConfirmation(null)}
        title={confirmation?.action === "restore_object_version" ? "Restore object version" : "Delete object version"}
        description={
          confirmation?.action === "restore_object_version"
            ? "This version becomes a new current object version."
            : "This stored version cannot be recovered by AIPermission."
        }
        size="md"
        closeDisabled={pending}
      >
        <div className="grid gap-4">
          <div className={`grid gap-2 rounded-md border p-3 ${detailClass}`}>
            <p className="break-all text-xs">
              <strong>Object:</strong> {objectKey}
            </p>
            <p className="break-all text-xs">
              <strong>Version:</strong> {confirmation?.version?.version_id}
            </p>
          </div>
          <Notice tone={confirmation?.action === "delete_object_version" ? "bad" : "warn"}>
            {confirmation?.action === "delete_object_version"
              ? "Permanent deletion requires explicit confirmation."
              : "The existing current version remains in version history."}
          </Notice>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={() => setConfirmation(null)} disabled={pending}>
              Cancel
            </Button>
            <Button
              type="button"
              variant={confirmation?.action === "delete_object_version" ? "danger" : "default"}
              onClick={confirmAction}
              disabled={pending}
            >
              {pending ? "Working..." : confirmation?.action === "delete_object_version" ? "Delete version" : "Restore version"}
            </Button>
          </div>
        </div>
      </Dialog>
    </>
  );
}

export function VersionsIcon() {
  return <History className="h-3.5 w-3.5" />;
}
