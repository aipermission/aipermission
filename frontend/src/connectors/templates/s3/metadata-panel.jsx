import { CopyButton } from "../../../components/ui/copy-button";
import { Input } from "../../../components/ui/form";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { formatBytes } from "../../../lib/file-transfer-utils";
import { HighlightedText } from "../_shared/highlighted-text";

export function S3MetadataPanel({
  metadata,
  selectedKey,
  directories,
  objects,
  visibleBytes,
  prefix,
  search,
  metadataSearch,
  onMetadataSearch,
  inputClass,
}) {
  if (!metadata) {
    return (
      <TerminalBlock className="min-h-0 text-xs" surface="log">
        {selectedKey ? "Reading object metadata..." : "Select an object to inspect metadata, or use Bucket info for endpoint details."}
      </TerminalBlock>
    );
  }
  const isBucketInfo = !metadata.key && Boolean(metadata.bucket);
  const cards = isBucketInfo
    ? [
        { label: "Bucket", value: metadata.bucket || "unknown" },
        { label: "Endpoint", value: metadata.endpoint || "unknown" },
        { label: "Region", value: metadata.region || "unknown" },
        { label: "Visible folders", value: String(directories.length) },
        { label: "Visible objects", value: String(objects.length) },
        { label: "Visible size", value: formatBytes(visibleBytes) },
        { label: "Current prefix", value: prefix || "bucket root" },
        { label: "Search", value: search || "none" },
        { label: "Request id", value: metadata.headers?.["X-Amz-Request-Id"] || metadata.headers?.["X-Amz-Id-2"] || "not provided" },
      ]
    : [
        { label: "Object", value: metadata.key || selectedKey || "unknown" },
        { label: "Bucket", value: metadata.bucket || "unknown" },
        { label: "Size", value: formatBytes(metadata.content_length) },
        { label: "Content type", value: metadata.content_type || "unknown" },
        { label: "Last modified", value: metadata.last_modified || "unknown" },
        { label: "ETag", value: metadata.etag || "unknown" },
      ];
  const rawValue = JSON.stringify(metadata, null, 2);
  return (
    <div className="grid min-h-0 grid-rows-[auto_minmax(0,450px)_auto_minmax(0,1fr)] overflow-hidden">
      <S3ResultHeader
        title={isBucketInfo ? "Bucket summary" : "Object summary"}
        subtitle={isBucketInfo ? "visible listing stats" : "metadata"}
      />
      <S3MetadataSummary cards={cards} />
      <div className="mt-3 flex items-center justify-between gap-3">
        <p className="truncate text-xs font-semibold uppercase tracking-wide text-stone-500">S3 raw data</p>
        <div className="flex min-w-0 items-center justify-end gap-2">
          <Input
            className={`h-8 w-56 text-xs ${inputClass || ""}`}
            value={metadataSearch}
            onChange={(event) => onMetadataSearch?.(event.target.value)}
            placeholder="Search raw data"
          />
          <CopyButton value={rawValue} variant="outline" className="h-8 px-2 text-xs" />
        </div>
      </div>
      <div className="mt-2 grid min-h-0 overflow-hidden">
        <TerminalBlock className="min-h-0 whitespace-pre-wrap break-words text-xs [overflow-wrap:anywhere]" surface="dark">
          <HighlightedText text={rawValue} query={metadataSearch} />
        </TerminalBlock>
      </div>
    </div>
  );
}

function S3ResultHeader({ title, subtitle }) {
  return (
    <div className="mb-2 flex items-center justify-between gap-3">
      <div className="min-w-0">
        <p className="truncate text-xs font-semibold uppercase tracking-wide text-stone-500">{title}</p>
        {subtitle ? <p className="truncate text-xs text-stone-500">{subtitle}</p> : null}
      </div>
    </div>
  );
}

function S3MetadataSummary({ cards }) {
  return (
    <div className="min-h-0 overflow-auto rounded-md border border-stone-700 bg-[#1a1a1a] p-3">
      <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
        {cards.map((card) => (
          <div key={card.label} className="min-w-0 rounded border border-stone-700 bg-[#202020] p-2">
            <p className="text-[10px] font-semibold uppercase tracking-wide text-stone-500">{card.label}</p>
            <p className="mt-1 whitespace-pre-wrap break-words font-mono text-xs text-stone-100">{String(card.value)}</p>
          </div>
        ))}
      </div>
    </div>
  );
}
