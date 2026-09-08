import { CornerUpLeft, Database, Folder, Plus, RefreshCcw, Search, Upload } from "lucide-react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { Input } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { formatBytes } from "../../../lib/file-transfer-utils";
import { parentPrefix, shortDate } from "./helpers";

export function S3ObjectBrowser({
  target,
  directories,
  objects,
  prefix,
  search,
  selectedKey,
  nextToken,
  latestAction,
  state,
  classes,
  onPrefixChange,
  onSearchChange,
  onSearch,
  onBucketInfo,
  onOpenTransfer,
  onOpenUpload,
  onRefresh,
  onOpenParent,
  onOpenDirectory,
  onSelectObject,
  onLoadMore,
}) {
  const disabled = state.state !== "idle";
  return (
    <section
      className={`grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden rounded-lg border ${classes.border} ${classes.subtlePanel}`}
    >
      <ObjectBrowserHeader
        target={target}
        count={directories.length + objects.length}
        latestAction={latestAction}
        disabled={disabled}
        classes={classes}
        onBucketInfo={onBucketInfo}
        onOpenTransfer={onOpenTransfer}
        onOpenUpload={onOpenUpload}
        onRefresh={onRefresh}
      />
      <ObjectBrowserSearch
        prefix={prefix}
        search={search}
        loading={state.state === "loading"}
        disabled={disabled}
        classes={classes}
        onPrefixChange={onPrefixChange}
        onSearchChange={onSearchChange}
        onSearch={onSearch}
      />
      <ObjectBrowserList
        directories={directories}
        objects={objects}
        prefix={prefix}
        search={search}
        selectedKey={selectedKey}
        loading={state.state === "loading"}
        classes={classes}
        onOpenParent={onOpenParent}
        onOpenDirectory={onOpenDirectory}
        onSelectObject={onSelectObject}
      />
      <div className={`flex items-center justify-between gap-2 border-t p-3 ${classes.border}`}>
        <span className={`text-xs ${classes.muted}`}>{nextToken ? "More objects available" : "End of current listing"}</span>
        <Button type="button" variant="outline" className="h-8" disabled={!nextToken || disabled} onClick={onLoadMore}>
          Load more
        </Button>
      </div>
    </section>
  );
}

function ObjectBrowserHeader({ target, count, latestAction, disabled, classes, onBucketInfo, onOpenTransfer, onOpenUpload, onRefresh }) {
  return (
    <div className={`border-b p-3 ${classes.border}`}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <p className="text-sm font-semibold">Objects</p>
          <p className={`text-xs ${classes.muted}`}>
            {count} loaded · {target.config?.bucket || "bucket"}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {latestAction ? (
            <Badge tone={latestAction.status === "failed" ? "bad" : latestAction.status === "completed" ? "good" : "warn"}>
              {latestAction.action_name}
            </Badge>
          ) : null}
          <IconButton label="Bucket info" disabled={disabled} onClick={onBucketInfo} icon={Database} />
          <IconButton
            label="Transfer files and folders"
            disabled={disabled || !target.transfer_runtime_id}
            onClick={onOpenTransfer}
            icon={Upload}
          />
          <IconButton label="Create a small object" disabled={disabled} onClick={onOpenUpload} icon={Plus} />
          <IconButton label="Refresh objects" disabled={disabled} onClick={onRefresh} icon={RefreshCcw} />
        </div>
      </div>
    </div>
  );
}

function ObjectBrowserSearch({ prefix, search, loading, disabled, classes, onPrefixChange, onSearchChange, onSearch }) {
  return (
    <form
      className={`grid gap-2 border-b p-3 ${classes.border}`}
      onSubmit={(event) => {
        event.preventDefault();
        onSearch();
      }}
    >
      <Input
        className={classes.input}
        value={prefix}
        onChange={(event) => onPrefixChange(event.target.value)}
        placeholder="Prefix, e.g. backups/2026/"
      />
      <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-2">
        <div className="relative">
          <Search className={`pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 ${classes.muted}`} />
          <Input
            className={`pl-9 ${classes.input}`}
            value={search}
            onChange={(event) => onSearchChange(event.target.value)}
            placeholder="Search object keys"
          />
        </div>
        <Button type="submit" variant="outline" className="h-10" disabled={disabled}>
          {loading ? "Loading" : "Search"}
        </Button>
      </div>
    </form>
  );
}

function ObjectBrowserList({
  directories,
  objects,
  prefix,
  search,
  selectedKey,
  loading,
  classes,
  onOpenParent,
  onOpenDirectory,
  onSelectObject,
}) {
  return (
    <div className="min-h-0 overflow-auto p-2">
      {prefix && !search ? (
        <button
          type="button"
          className={`mb-1 flex w-full items-center gap-3 rounded-md border px-3 py-2 text-left text-sm transition ${classes.border} ${classes.rowHover}`}
          onClick={onOpenParent}
        >
          <CornerUpLeft className={`h-4 w-4 shrink-0 ${classes.muted}`} />
          <span className="min-w-0">
            <span className="block truncate font-semibold">..</span>
            <span className={`block truncate text-xs ${classes.muted}`}>{parentPrefix(prefix) || "bucket root"}</span>
          </span>
        </button>
      ) : null}
      {!search
        ? directories.map((directory) => (
            <button
              key={directory.prefix}
              type="button"
              className={`mb-1 flex w-full items-center gap-3 rounded-md border px-3 py-2 text-left text-sm transition ${classes.border} ${classes.rowHover}`}
              onClick={() => onOpenDirectory(directory.prefix)}
            >
              <Folder className="h-4 w-4 shrink-0 text-amber-400" />
              <span className="min-w-0">
                <span className="block truncate font-mono text-xs font-semibold" title={directory.prefix}>
                  {directory.name || directory.prefix}
                </span>
                <span className={`block truncate text-xs ${classes.muted}`}>{directory.prefix}</span>
              </span>
            </button>
          ))
        : null}
      {objects.map((object) => {
        const active = selectedKey === object.key;
        return (
          <button
            key={object.key}
            type="button"
            className={`mb-1 grid w-full gap-1 rounded-md border px-3 py-2 text-left text-sm transition ${active ? classes.activeRow : `${classes.border} ${classes.rowHover}`}`}
            onClick={() => onSelectObject(object.key)}
            aria-pressed={active}
          >
            <span className="truncate font-mono text-xs font-semibold" title={object.key}>
              {object.key}
            </span>
            <span className={`text-xs ${active ? "" : classes.muted}`}>
              {formatBytes(object.size)} · {shortDate(object.last_modified)}
            </span>
          </button>
        );
      })}
      {directories.length === 0 && objects.length === 0 ? (
        <Notice>{loading ? "Loading S3 objects..." : "No objects found for this prefix/search."}</Notice>
      ) : null}
    </div>
  );
}

function IconButton({ label, disabled, onClick, icon: Icon }) {
  return (
    <Button type="button" variant="outline" className="h-8 w-8 px-0" title={label} onClick={onClick} disabled={disabled}>
      <Icon className="h-3.5 w-3.5" />
    </Button>
  );
}
