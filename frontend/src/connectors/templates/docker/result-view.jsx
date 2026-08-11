import { CopyButton } from "../../../components/ui/copy-button";
import { Input } from "../../../components/ui/form";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import {
  arrayOrString,
  formatDockerLogs,
  resourceSecondary,
  resourceSingular,
  stripSlash,
  summarizeNetworks,
  summarizePorts,
} from "./helpers";

export function DockerResultView({ item, search, onSearch, inputClass }) {
  const output = item.output || {};
  const isLogs = item.action_name === "container_logs" && output.logs;
  const isInspect = item.action_name === "inspect_container";
  const text = isLogs ? formatDockerLogs(output.logs) : JSON.stringify(output, null, 2);
  const copyValue = output.logs ? output.logs : JSON.stringify(output, null, 2);
  const title = dockerResultTitle(item);
  const subtitle = dockerResultSubtitle(item, output);
  if (isInspect) {
    const rawValue = JSON.stringify(output, null, 2);
    return (
      <div className="grid min-h-0 grid-rows-[auto_minmax(0,450px)_auto_minmax(0,1fr)] overflow-hidden">
        <DockerResultHeader title={title} subtitle={subtitle} />
        <DockerInspectSummary output={output} />
        <div className="mt-3 flex items-center justify-between gap-3">
          <p className="truncate text-xs font-semibold uppercase tracking-wide text-stone-500">Docker inspect raw data</p>
          <div className="flex min-w-0 items-center justify-end gap-2">
            <Input
              className={`h-8 w-56 text-xs ${inputClass || ""}`}
              value={search}
              onChange={(event) => onSearch?.(event.target.value)}
              placeholder="Search raw data"
            />
            <CopyButton value={rawValue} variant="outline" className="h-8 px-2 text-xs" />
          </div>
        </div>
        <div className="mt-2 grid min-h-0 overflow-hidden">
          <TerminalBlock className="min-h-0 whitespace-pre-wrap break-words text-xs [overflow-wrap:anywhere]" surface="dark">
            <HighlightedText text={rawValue} query={search} />
          </TerminalBlock>
        </div>
      </div>
    );
  }
  return (
    <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden">
      <DockerResultHeader
        title={title}
        subtitle={subtitle}
        copyValue={copyValue}
        search={search}
        onSearch={onSearch}
        inputClass={inputClass}
      />
      <TerminalBlock
        className={
          isLogs
            ? "h-full min-h-0 max-h-full overflow-auto whitespace-pre text-xs"
            : "min-h-0 whitespace-pre-wrap break-words text-xs [overflow-wrap:anywhere]"
        }
        surface={isLogs ? "log" : "dark"}
        style={
          isLogs
            ? { whiteSpace: "pre", overflowWrap: "normal", wordBreak: "normal" }
            : { whiteSpace: "pre-wrap", overflowWrap: "anywhere", wordBreak: "break-word" }
        }
      >
        <HighlightedText text={text} query={search} />
      </TerminalBlock>
    </div>
  );
}

function DockerResultHeader({ title, subtitle, copyValue, search, onSearch, inputClass }) {
  return (
    <div className="mb-2 flex items-center justify-between gap-3">
      <div className="min-w-0">
        <p className="truncate text-xs font-semibold uppercase tracking-wide text-stone-500">{title}</p>
        {subtitle ? <p className="truncate text-xs text-stone-500">{subtitle}</p> : null}
      </div>
      <div className="flex min-w-0 items-center justify-end gap-2">
        {onSearch ? (
          <Input
            className={`h-8 w-56 text-xs ${inputClass || ""}`}
            value={search}
            onChange={(event) => onSearch(event.target.value)}
            placeholder="Search logs"
          />
        ) : null}
        {copyValue ? <CopyButton value={copyValue} variant="outline" className="h-8 px-2 text-xs" /> : null}
      </div>
    </div>
  );
}

function HighlightedText({ text, query }) {
  const value = String(text || "");
  const needle = String(query || "");
  if (!needle.trim()) return value;
  const lowerValue = value.toLowerCase();
  const lowerNeedle = needle.toLowerCase();
  const parts = [];
  let index = 0;
  let matchIndex = lowerValue.indexOf(lowerNeedle, index);
  let key = 0;
  while (matchIndex !== -1) {
    if (matchIndex > index) parts.push(value.slice(index, matchIndex));
    parts.push(
      <mark key={`m-${key++}`} className="rounded bg-yellow-300 px-0.5 text-stone-950">
        {value.slice(matchIndex, matchIndex + needle.length)}
      </mark>,
    );
    index = matchIndex + needle.length;
    matchIndex = lowerValue.indexOf(lowerNeedle, index);
  }
  if (index < value.length) parts.push(value.slice(index));
  return parts;
}

function dockerResultTitle(item) {
  if (item.action_name === "container_logs") return "Container logs";
  if (item.action_name === "inspect_container") return "Docker inspect metadata";
  return String(item.action_name || "Docker action").replaceAll("_", " ");
}

function dockerResultSubtitle(item, output) {
  if (item.action_name === "container_logs") {
    const container = output.container || {};
    const name = container.name || container.id || "";
    const tail = output.tail ? `tail ${output.tail}` : "";
    return [name, tail].filter(Boolean).join(" · ");
  }
  if (item.action_name === "inspect_container") {
    const container = output.container || {};
    return container.name || container.id || "";
  }
  return item.display_text || "";
}

function DockerInspectSummary({ output }) {
  const inspect = Array.isArray(output.inspect) ? output.inspect[0] || {} : {};
  const container = output.container || {};
  const state = inspect.State || {};
  const config = inspect.Config || {};
  const hostConfig = inspect.HostConfig || {};
  const networkSettings = inspect.NetworkSettings || {};
  const ports = summarizePorts(networkSettings.Ports);
  const networks = summarizeNetworks(networkSettings.Networks);
  const mounts = Array.isArray(inspect.Mounts) ? inspect.Mounts : [];
  const labels = config.Labels && typeof config.Labels === "object" ? config.Labels : {};
  const health = state.Health || {};
  const rows = [
    ["Name", stripSlash(inspect.Name) || container.name],
    ["Image", config.Image || container.image || inspect.Image],
    [
      "State",
      [state.Status || container.state, state.Running === true ? "running" : "", state.Restarting === true ? "restarting" : ""]
        .filter(Boolean)
        .join(" / "),
    ],
    ["Status", container.status],
    ["Created", inspect.Created],
    ["Started", state.StartedAt],
    ["Finished", state.FinishedAt],
    ["Exit code", state.ExitCode],
    ["Health", health.Status],
    ["Restart count", inspect.RestartCount],
    ["Entrypoint", arrayOrString(config.Entrypoint)],
    ["Command", arrayOrString(config.Cmd)],
    ["Working dir", config.WorkingDir],
    ["User", config.User],
    ["Network mode", hostConfig.NetworkMode],
    ["Networks", networks],
    ["Ports", ports],
    [
      "Mounts",
      mounts
        .map((mount) => `${mount.Type || "mount"} ${mount.Source || ""} -> ${mount.Destination || ""}`)
        .filter(Boolean)
        .join("\n"),
    ],
    ["Labels", Object.keys(labels).length ? `${Object.keys(labels).length} labels` : ""],
  ].filter(([, value]) => value !== undefined && value !== null && String(value).trim() !== "");

  return (
    <div className="min-h-0 overflow-auto rounded-md border border-stone-700 bg-[#1a1a1a] p-3">
      <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
        {rows.map(([label, value]) => (
          <div key={label} className="min-w-0 rounded border border-stone-700 bg-[#202020] p-2">
            <p className="text-[10px] font-semibold uppercase tracking-wide text-stone-500">{label}</p>
            <p className="mt-1 whitespace-pre-wrap break-words font-mono text-xs text-stone-100">{String(value)}</p>
          </div>
        ))}
      </div>
    </div>
  );
}

export function DockerResourceDetail({ resourceView, item, search, onSearch, inputClass }) {
  const rawValue = JSON.stringify(item || {}, null, 2);
  const rows = resourceDetailRows(resourceView, item);
  return (
    <div className="grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden">
      <DockerResultHeader title={`${resourceSingular(resourceView)} metadata`} subtitle={resourceSecondary(resourceView, item)} />
      <div className="mb-3 min-h-0 overflow-auto rounded-md border border-stone-700 bg-[#1a1a1a] p-3">
        <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
          {rows.map(([label, value]) => (
            <div key={label} className="min-w-0 rounded border border-stone-700 bg-[#202020] p-2">
              <p className="text-[10px] font-semibold uppercase tracking-wide text-stone-500">{label}</p>
              <p className="mt-1 whitespace-pre-wrap break-words font-mono text-xs text-stone-100">{String(value)}</p>
            </div>
          ))}
        </div>
      </div>
      <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden">
        <div className="mb-2 flex items-center justify-between gap-3">
          <p className="truncate text-xs font-semibold uppercase tracking-wide text-stone-500">{resourceSingular(resourceView)} raw data</p>
          <div className="flex min-w-0 items-center justify-end gap-2">
            <Input
              className={`h-8 w-56 text-xs ${inputClass || ""}`}
              value={search}
              onChange={(event) => onSearch?.(event.target.value)}
              placeholder="Search raw data"
            />
            <CopyButton value={rawValue} variant="outline" className="h-8 px-2 text-xs" />
          </div>
        </div>
        <TerminalBlock className="min-h-0 whitespace-pre-wrap break-words text-xs [overflow-wrap:anywhere]" surface="dark">
          <HighlightedText text={rawValue} query={search} />
        </TerminalBlock>
      </div>
    </div>
  );
}

function resourceDetailRows(kind, item = {}) {
  if (kind === "images") {
    return [
      ["Repository", item.repository],
      ["Tag", item.tag],
      ["Image ID", item.id],
      ["Digest", item.digest],
      ["Size", item.size],
      ["Created", item.created_since || item.created_at],
      ["Visible containers", item.containers ?? 0],
    ].filter(([, value]) => value !== undefined && value !== null && String(value).trim() !== "");
  }
  if (kind === "networks") {
    return [
      ["Name", item.name],
      ["Network ID", item.id],
      ["Driver", item.driver],
      ["Scope", item.scope],
      ["IPv6", item.ipv6],
      ["Internal", item.internal],
      ["Visible containers", item.containers ?? 0],
      ["Labels", item.labels],
    ].filter(([, value]) => value !== undefined && value !== null && String(value).trim() !== "");
  }
  if (kind === "volumes") {
    return [
      ["Name", item.name],
      ["Driver", item.driver],
      ["Scope", item.scope],
      ["Mountpoint", item.mountpoint],
      ["Visible containers", item.containers ?? 0],
      ["Labels", item.labels],
    ].filter(([, value]) => value !== undefined && value !== null && String(value).trim() !== "");
  }
  return Object.entries(item).map(([key, value]) => [key, typeof value === "string" ? value : JSON.stringify(value)]);
}
