import { FileUp, X } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Dialog } from "../../../components/ui/dialog";
import { Field, Input, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { formatBytes } from "../../../lib/file-transfer-utils";
import { joinObjectKey, normalizeObjectKey } from "./helpers";

export const defaultUploadDialog = Object.freeze({
  open: false,
  mode: "files",
  prefix: "",
  files: [],
  textKey: "",
  textContent: "",
  textContentType: "text/plain",
  overwrite: false,
  pending: false,
  error: "",
});
export const defaultS3ConfirmDialog = Object.freeze({
  open: false,
  title: "",
  description: "",
  details: [],
  action: null,
  pending: false,
  danger: false,
});

export function S3UploadDialog({
  value,
  theme,
  inputClass,
  borderClass,
  mutedClass,
  subtlePanelClass,
  onClose,
  onChange,
  onFiles,
  onRemoveFile,
  onUpdateFile,
  onSubmit,
}) {
  if (!value.open) return null;
  const darkTextClass = theme === "light" ? "" : "text-stone-200";
  const fileMode = value.mode !== "text";
  const canUpload = fileMode
    ? value.files.some((item) => normalizeObjectKey(item.key))
    : Boolean(normalizeObjectKey(value.textKey) && value.textContent);
  const uploadCount = fileMode ? value.files.filter((item) => normalizeObjectKey(item.key)).length : canUpload ? 1 : 0;
  return (
    <Dialog
      open={value.open}
      onClose={onClose}
      title="Upload S3 objects"
      description="Upload local files or create a small text object."
      size="wide"
      closeDisabled={value.pending}
      closeOnOverlay={false}
      closeOnEscape={false}
    >
      <form
        className="grid max-h-[calc(100vh-150px)] min-h-0 gap-4 overflow-hidden lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]"
        onSubmit={onSubmit}
      >
        <section className="grid min-h-0 content-start gap-4">
          <Notice tone="warn">
            Uploads run as bounded S3 connector write actions. Keep write permissions in Prompt mode until the workflow is trusted.
          </Notice>
          <div className={`grid grid-cols-2 rounded-lg border p-1 ${borderClass} ${subtlePanelClass}`}>
            <button
              type="button"
              className={`rounded-md px-3 py-2 text-sm font-semibold ${fileMode ? "bg-emerald-600 text-white" : mutedClass}`}
              onClick={() => onChange((current) => ({ ...current, mode: "files", error: "" }))}
              disabled={value.pending}
            >
              File upload
            </button>
            <button
              type="button"
              className={`rounded-md px-3 py-2 text-sm font-semibold ${!fileMode ? "bg-emerald-600 text-white" : mutedClass}`}
              onClick={() => onChange((current) => ({ ...current, mode: "text", error: "" }))}
              disabled={value.pending}
            >
              Create text object
            </button>
          </div>
          <div className={`grid gap-3 rounded-lg border p-3 ${borderClass} ${subtlePanelClass}`}>
            {fileMode ? (
              <>
                <Field className={darkTextClass}>
                  Object prefix
                  <Input
                    className={inputClass}
                    value={value.prefix}
                    onChange={(event) => {
                      const nextPrefix = event.target.value;
                      onChange((current) => ({
                        ...current,
                        prefix: nextPrefix,
                        files: current.files.map((item) => ({ ...item, key: joinObjectKey(nextPrefix, item.file.name) })),
                      }));
                    }}
                    placeholder="folder/subfolder/"
                  />
                </Field>
                <Field className={darkTextClass}>
                  Add files
                  <Input className={inputClass} type="file" multiple onChange={(event) => onFiles(event.target.files)} />
                </Field>
              </>
            ) : (
              <>
                <Field className={darkTextClass}>
                  Object key
                  <Input
                    className={inputClass}
                    value={value.textKey}
                    onChange={(event) => onChange((current) => ({ ...current, textKey: event.target.value }))}
                    placeholder="notes/readme.txt"
                  />
                </Field>
                <Field className={darkTextClass}>
                  Content type
                  <Input
                    className={inputClass}
                    value={value.textContentType}
                    onChange={(event) => onChange((current) => ({ ...current, textContentType: event.target.value }))}
                    placeholder="text/plain"
                  />
                </Field>
                <Field className={darkTextClass}>
                  Content
                  <Textarea
                    className={`${inputClass} min-h-36 font-mono text-xs`}
                    value={value.textContent}
                    onChange={(event) => onChange((current) => ({ ...current, textContent: event.target.value }))}
                    placeholder="Text content"
                  />
                </Field>
              </>
            )}
            <label className={`flex items-center gap-2 rounded-md border px-3 py-2 text-sm ${borderClass}`}>
              <input
                type="checkbox"
                checked={value.overwrite}
                onChange={(event) => onChange((current) => ({ ...current, overwrite: event.target.checked }))}
              />
              overwrite existing objects
            </label>
          </div>
        </section>
        <section className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] gap-3">
          <div>
            <p className="text-sm font-semibold">{fileMode ? "Upload queue" : "Text object preview"}</p>
            <p className={`text-xs ${mutedClass}`}>
              {fileMode
                ? `${value.files.length} file${value.files.length === 1 ? "" : "s"} selected`
                : normalizeObjectKey(value.textKey) || "No object key yet"}
            </p>
          </div>
          <div className={`min-h-0 overflow-auto rounded-lg border ${borderClass}`}>
            {!fileMode ? (
              <div className="grid gap-3 p-3">
                <p className="break-all font-mono text-xs">{normalizeObjectKey(value.textKey) || "notes/readme.txt"}</p>
                <TerminalBlock className="min-h-48 text-xs" surface="log">
                  {value.textContent || "Text content preview will appear here."}
                </TerminalBlock>
              </div>
            ) : value.files.length === 0 ? (
              <div className="grid h-full min-h-48 place-items-center p-6 text-center">
                <p className={`text-sm ${mutedClass}`}>Selected files will appear here with editable object keys.</p>
              </div>
            ) : (
              <div className="grid divide-y divide-stone-200 dark:divide-stone-700">
                {value.files.map((item) => (
                  <div className="grid gap-2 p-3" key={item.id}>
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <p className="truncate text-sm font-semibold" title={item.file.name}>
                          {item.file.name}
                        </p>
                        <p className={`text-xs ${mutedClass}`}>
                          {formatBytes(item.file.size)} · {item.contentType || "application/octet-stream"}
                        </p>
                      </div>
                      <Button
                        type="button"
                        variant="ghost"
                        className="h-8 w-8 px-0"
                        title="Remove file"
                        onClick={() => onRemoveFile(item.id)}
                        disabled={value.pending}
                      >
                        <X className="h-4 w-4" />
                      </Button>
                    </div>
                    <Input
                      className={inputClass}
                      value={item.key}
                      onChange={(event) => onUpdateFile(item.id, { key: event.target.value })}
                      disabled={value.pending}
                    />
                  </div>
                ))}
              </div>
            )}
          </div>
          {value.error ? <Notice tone="bad">{value.error}</Notice> : null}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose} disabled={value.pending}>
              Cancel
            </Button>
            <Button type="submit" disabled={!canUpload || value.pending}>
              <FileUp className="h-4 w-4" />
              {value.pending ? "Uploading..." : `Upload ${uploadCount} object(s)`}
            </Button>
          </div>
        </section>
      </form>
    </Dialog>
  );
}

export function S3ConfirmDialog({ value, theme, onClose, onConfirm }) {
  if (!value.open) return null;
  const detailClass = theme === "light" ? "border-stone-200 bg-stone-50 text-stone-700" : "border-stone-700 bg-stone-900 text-stone-200";
  return (
    <Dialog open={value.open} onClose={onClose} title={value.title} description={value.description} size="md" closeDisabled={value.pending}>
      <div className="grid gap-4">
        <div className={`grid gap-2 rounded-md border p-3 text-sm ${detailClass}`}>
          {value.details.map((detail) => (
            <div className="grid gap-1" key={detail.label}>
              <span className="text-xs font-semibold uppercase tracking-wide opacity-70">{detail.label}</span>
              <span className="break-all font-mono text-xs">{detail.value}</span>
            </div>
          ))}
        </div>
        {value.danger ? (
          <Notice tone="bad">This action changes object storage state and cannot be undone by AIPermission.</Notice>
        ) : (
          <Notice tone="warn">Review the object keys before continuing.</Notice>
        )}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose} disabled={value.pending}>
            Cancel
          </Button>
          <Button type="button" variant={value.danger ? "danger" : "default"} onClick={onConfirm} disabled={value.pending}>
            {value.pending ? "Working..." : value.danger ? "Delete" : "Confirm"}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
