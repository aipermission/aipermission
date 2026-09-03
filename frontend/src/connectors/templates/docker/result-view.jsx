import { TerminalBlock } from "../../../components/ui/terminal-block";
import { HighlightedText } from "../_shared/highlighted-text";
import { ConnectorResultHeader, DarkSummaryGrid } from "../_shared/result-sections";
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
        <ConnectorResultHeader title={title} subtitle={subtitle} />
        <DockerInspectSummary output={output} />
        <div className="mt-3">
          <ConnectorResultHeader
            title="Docker inspect raw data"
            copyValue={rawValue}
            search={search}
            onSearch={onSearch}
            inputClass={inputClass}
            searchPlaceholder="Search raw data"
          />
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
      <ConnectorResultHeader
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
      >
        <HighlightedText text={text} query={search} />
      </TerminalBlock>
    </div>
  );
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

  return <DarkSummaryGrid rows={rows.map(([label, value]) => ({ label, value }))} />;
}

export function DockerResourceDetail({ resourceView, item, search, onSearch, inputClass }) {
  const rawValue = JSON.stringify(item || {}, null, 2);
  const rows = resourceDetailRows(resourceView, item);
  return (
    <div className="grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden">
      <ConnectorResultHeader title={`${resourceSingular(resourceView)} metadata`} subtitle={resourceSecondary(resourceView, item)} />
      <div className="mb-3 min-h-0 overflow-hidden">
        <DarkSummaryGrid rows={rows.map(([label, value]) => ({ label, value }))} />
      </div>
      <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden">
        <ConnectorResultHeader
          title={`${resourceSingular(resourceView)} raw data`}
          copyValue={rawValue}
          search={search}
          onSearch={onSearch}
          inputClass={inputClass}
          searchPlaceholder="Search raw data"
        />
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
