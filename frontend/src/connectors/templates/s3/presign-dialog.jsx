import { Link2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Dialog } from "../../../components/ui/dialog";
import { Field, Input } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";

const defaultExpirySeconds = 900;

export function S3PresignDialog({ open, selectedKey, theme, inputClass, borderClass, mutedClass, onClose, onRun }) {
  const [mode, setMode] = useState("download");
  const [key, setKey] = useState("");
  const [expiresSeconds, setExpiresSeconds] = useState(defaultExpirySeconds);
  const [overwrite, setOverwrite] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState(null);

  useEffect(() => {
    if (!open) return;
    setMode("download");
    setKey(selectedKey || "");
    setExpiresSeconds(defaultExpirySeconds);
    setOverwrite(false);
    setPending(false);
    setError("");
    setResult(null);
  }, [open, selectedKey]);

  if (!open) return null;

  async function submit(event) {
    event.preventDefault();
    const normalizedKey = key.trim().replace(/^\/+/, "");
    const normalizedExpiry = Number(expiresSeconds);
    if (!normalizedKey) {
      setError("Object key is required.");
      return;
    }
    if (!Number.isInteger(normalizedExpiry) || normalizedExpiry < 60 || normalizedExpiry > 3600) {
      setError("Expiry must be between 60 and 3600 seconds.");
      return;
    }
    setPending(true);
    setError("");
    setResult(null);
    try {
      const item = await onRun({
        actionName: mode === "upload" ? "presign_upload" : "presign_download",
        input: {
          key: normalizedKey,
          expires_seconds: normalizedExpiry,
          ...(mode === "upload" ? { overwrite } : {}),
        },
        reason: `manual S3 presigned ${mode} URL`,
        busy: "signing",
      });
      setResult(item.output || null);
    } catch (runError) {
      setError(runError.message || "Presigned URL creation failed.");
    } finally {
      setPending(false);
    }
  }

  const darkTextClass = theme === "light" ? "" : "text-stone-200";
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Create temporary S3 URL"
      description="Grant short-lived access to one exact object key without sharing the S3 credential."
      size="md"
      closeDisabled={pending}
      closeOnOverlay={false}
    >
      <form className="grid gap-4" onSubmit={submit}>
        <div className={`grid grid-cols-2 rounded-lg border p-1 ${borderClass}`}>
          {[
            ["download", "Download"],
            ["upload", "Upload"],
          ].map(([value, label]) => (
            <button
              type="button"
              key={value}
              className={`rounded-md px-3 py-2 text-sm font-semibold ${mode === value ? "bg-emerald-600 text-white" : mutedClass}`}
              onClick={() => {
                setMode(value);
                setError("");
                setResult(null);
              }}
              disabled={pending}
            >
              {label}
            </button>
          ))}
        </div>
        <Field className={darkTextClass}>
          Object key
          <Input className={inputClass} value={key} onChange={(event) => setKey(event.target.value)} placeholder="folder/object.ext" />
        </Field>
        <Field className={darkTextClass}>
          Expires in seconds
          <Input
            className={inputClass}
            type="number"
            min="60"
            max="3600"
            step="60"
            value={expiresSeconds}
            onChange={(event) => setExpiresSeconds(event.target.value)}
          />
        </Field>
        {mode === "upload" ? (
          <label className={`flex items-center gap-2 rounded-md border px-3 py-2 text-sm ${borderClass}`}>
            <input type="checkbox" checked={overwrite} onChange={(event) => setOverwrite(event.target.checked)} disabled={pending} />
            allow replacing an existing object
          </label>
        ) : null}
        <Notice tone="warn">The generated URL is a temporary bearer credential. Anyone holding it can use the approved operation until it expires.</Notice>
        {error ? <Notice tone="bad">{error}</Notice> : null}
        {result?.url ? (
          <div className={`grid gap-2 rounded-lg border p-3 ${borderClass}`}>
            <div className="flex items-center justify-between gap-3">
              <div className="min-w-0">
                <p className="text-sm font-semibold">{result.operation === "upload" ? "Upload URL" : "Download URL"}</p>
                <p className={`truncate text-xs ${mutedClass}`}>Expires {result.expires_at}</p>
              </div>
              <CopyButton value={result.url} variant="outline" className="h-8 px-2 text-xs" />
            </div>
            <p className="max-h-24 overflow-auto break-all font-mono text-xs">{result.url}</p>
          </div>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose} disabled={pending}>
            Close
          </Button>
          <Button type="submit" disabled={pending || !key.trim()}>
            <Link2 className="h-4 w-4" />
            {pending ? "Creating..." : "Create URL"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
