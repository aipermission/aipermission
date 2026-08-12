export function S3EndpointFooter({ target, borderClass, mutedClass }) {
  const scheme = target.config?.scheme || "https";
  const host = target.config?.host || "s3.amazonaws.com";
  const port = target.config?.port || (scheme === "http" ? 80 : 443);
  const bucket = target.config?.bucket || "bucket";
  const mode =
    target.config?.connection_mode === "over_ssh" ? `over ssh · ${target.config?.transport_target_ref || "transport"}` : "direct";
  return (
    <div className={`flex items-center justify-between gap-3 border-t px-4 py-2 text-xs ${borderClass} ${mutedClass}`}>
      <span>S3 transport</span>
      <span className="truncate">
        {scheme}://{host}:{port}/{bucket} · {mode}
      </span>
    </div>
  );
}
