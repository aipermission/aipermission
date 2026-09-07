import { Download, Link2, Trash2 } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { formatBytes } from "../../../lib/file-transfer-utils";
import { shortDate } from "./helpers";
import { LifecycleIcon } from "./lifecycle-dialog";
import { S3MetadataPanel } from "./metadata-panel";
import { VersionsIcon } from "./versions-dialog";

export function S3ObjectDetailPane({
  active,
  selectedKey,
  selectedObject,
  metadata,
  directories,
  objects,
  visibleBytes,
  prefix,
  search,
  metadataSearch,
  state,
  classes,
  onMetadataSearch,
  onOpenLifecycle,
  onOpenPresign,
  onOpenVersions,
  onDownload,
  onDelete,
}) {
  const disabled = state.state !== "idle";
  return (
    <section
      className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${classes.border} ${classes.subtlePanel}`}
    >
      <div>
        <div className={`border-b p-3 ${classes.border}`}>
          <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
            <div className="min-w-0">
              <p className="truncate text-sm font-semibold">{selectedKey || "S3 object detail"}</p>
              <p className={`truncate text-xs ${classes.muted}`}>
                {selectedObject
                  ? `${formatBytes(selectedObject.size)} · ${shortDate(selectedObject.last_modified)}`
                  : "Select an object or upload a new one."}
              </p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <IconButton label="Bucket lifecycle" disabled={!active || disabled} onClick={onOpenLifecycle} icon={LifecycleIcon} />
              <IconButton label="Create temporary S3 URL" disabled={!active || disabled} onClick={onOpenPresign} icon={Link2} />
              <IconButton label="Object versions" disabled={!selectedKey || disabled} onClick={onOpenVersions} icon={VersionsIcon} />
              <IconButton label="Download object" disabled={!selectedKey || disabled} onClick={onDownload} icon={Download} />
              <IconButton label="Delete object" disabled={!selectedKey || disabled} onClick={onDelete} icon={Trash2} danger />
            </div>
          </div>
        </div>
        {state.error ? (
          <div className={`border-b px-3 py-2 text-right text-xs text-red-500 ${classes.border}`}>
            <span className="break-words">{state.error}</span>
          </div>
        ) : null}
      </div>
      <div className="grid h-full min-h-0 grid-rows-[minmax(0,1fr)] overflow-hidden p-3">
        <S3MetadataPanel
          metadata={metadata}
          selectedKey={selectedKey}
          directories={directories}
          objects={objects}
          visibleBytes={visibleBytes}
          prefix={prefix}
          search={search}
          metadataSearch={metadataSearch}
          onMetadataSearch={onMetadataSearch}
          inputClass={classes.input}
        />
      </div>
    </section>
  );
}

function IconButton({ label, disabled, onClick, icon: Icon, danger = false }) {
  return (
    <Button
      type="button"
      variant={danger ? "danger" : "outline"}
      className="h-8 w-8 px-0"
      title={label}
      disabled={disabled}
      onClick={onClick}
    >
      <Icon className="h-3.5 w-3.5" />
    </Button>
  );
}
