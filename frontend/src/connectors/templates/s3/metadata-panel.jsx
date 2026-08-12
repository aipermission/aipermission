import { TerminalBlock } from "../../../components/ui/terminal-block";
import { formatBytes } from "../../../lib/file-transfer-utils";
import { HighlightedText } from "../_shared/highlighted-text";
import { ConnectorResultHeader, DarkSummaryGrid } from "../_shared/result-sections";

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
      <ConnectorResultHeader
        title={isBucketInfo ? "Bucket summary" : "Object summary"}
        subtitle={isBucketInfo ? "visible listing stats" : "metadata"}
      />
      <DarkSummaryGrid rows={cards} />
      <div className="mt-3">
        <ConnectorResultHeader
          title="S3 raw data"
          copyValue={rawValue}
          search={metadataSearch}
          onSearch={onMetadataSearch}
          inputClass={inputClass}
          searchPlaceholder="Search raw data"
        />
      </div>
      <div className="mt-2 grid min-h-0 overflow-hidden">
        <TerminalBlock className="min-h-0 whitespace-pre-wrap break-words text-xs [overflow-wrap:anywhere]" surface="dark">
          <HighlightedText text={rawValue} query={metadataSearch} />
        </TerminalBlock>
      </div>
    </div>
  );
}
