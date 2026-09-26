import { TerminalBlock } from "../../../components/ui/terminal-block";
import { HighlightedText } from "../_shared/highlighted-text";
import { ConnectorResultHeader, DarkSummaryGrid, RawDataSection } from "../_shared/result-sections";
import {
  arrayOrString,
  formatDockerLogs,
  resourceSecondary,
  resourceSingular,
  stripSlash,
  summarizeNetworks,
  summarizePorts,
} from "./helpers";
import type { ConnectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import type { DockerResource, DockerResourceKind } from "./resource-types";
import { dockerOutputRecord as record } from "./resource-output";

type DockerViewItem = Pick<ConnectorActionResponse, "action_name"> & Partial<Pick<ConnectorActionResponse, "output" | "display_text">>;
type SearchProps = { search: string; onSearch: (_search: string) => void; inputClass: string };

function textValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function visibleSummaryRows(rows: [string, unknown][]) {
  return rows.filter(([, value]) => value !== undefined && value !== null && String(value).trim() !== "");
}

export function DockerResultView({ item, search, onSearch, inputClass }: SearchProps & { item: DockerViewItem }) {
  const rawOutput = item.output || {};
  const output = record(rawOutput);
  const logs = textValue(output.logs);
  const isLogs = item.action_name === "container_logs" && Boolean(logs);
  const isInspect = item.action_name === "inspect_container";
  const text = isLogs ? formatDockerLogs(logs) : JSON.stringify(rawOutput, null, 2);
  const copyValue = logs || JSON.stringify(rawOutput, null, 2);
  const title = dockerResultTitle(item);
  const subtitle = dockerResultSubtitle(item, output);
  if (isInspect) {
    const rawValue = JSON.stringify(rawOutput, null, 2);
    return (
      <div className="grid min-h-0 grid-rows-[auto_minmax(0,450px)_minmax(0,1fr)] overflow-hidden">
        <ConnectorResultHeader title={title} subtitle={subtitle} />
        <DockerInspectSummary output={output} />
        <RawDataSection
          title="Docker inspect raw data"
          value={rawValue}
          search={search}
          onSearch={onSearch}
          inputClass={inputClass}
          className="mt-3"
          blockClassName="mt-2"
        />
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

function dockerResultTitle(item: DockerViewItem) {
  if (item.action_name === "container_logs") return "Container logs";
  if (item.action_name === "inspect_container") return "Docker inspect metadata";
  return String(item.action_name || "Docker action").replaceAll("_", " ");
}

function dockerResultSubtitle(item: DockerViewItem, output: Record<string, unknown>) {
  if (item.action_name === "container_logs") {
    const container = record(output.container);
    const name = textValue(container.name) || textValue(container.id);
    const tail = output.tail ? `tail ${output.tail}` : "";
    return [name, tail].filter(Boolean).join(" · ");
  }
  if (item.action_name === "inspect_container") {
    const container = record(output.container);
    return textValue(container.name) || textValue(container.id);
  }
  return item.display_text || "";
}

function DockerInspectSummary({ output }: { output: Record<string, unknown> }) {
  const inspect = record(Array.isArray(output.inspect) ? output.inspect[0] : null);
  const container = record(output.container);
  const state = record(inspect.State);
  const config = record(inspect.Config);
  const hostConfig = record(inspect.HostConfig);
  const networkSettings = record(inspect.NetworkSettings);
  const ports = summarizePorts(record(networkSettings.Ports));
  const networks = summarizeNetworks(record(networkSettings.Networks));
  const mounts = Array.isArray(inspect.Mounts) ? inspect.Mounts : [];
  const labels = config.Labels && typeof config.Labels === "object" ? config.Labels : {};
  const health = record(state.Health);
  const rows: [string, unknown][] = [
    ["Name", stripSlash(textValue(inspect.Name)) || container.name],
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
        .map((value: unknown) => {
          const mount = record(value);
          return `${mount.Type || "mount"} ${mount.Source || ""} -> ${mount.Destination || ""}`;
        })
        .filter(Boolean)
        .join("\n"),
    ],
    ["Labels", Object.keys(labels).length ? `${Object.keys(labels).length} labels` : ""],
  ];

  return <DarkSummaryGrid rows={visibleSummaryRows(rows).map(([label, value]) => ({ label, value }))} />;
}

export function DockerResourceDetail({ resourceView, item, search, onSearch, inputClass }: SearchProps & { resourceView: DockerResourceKind; item: DockerResource }) {
  const rawValue = JSON.stringify(item || {}, null, 2);
  const rows = resourceDetailRows(resourceView, item);
  return (
    <div className="grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden">
      <ConnectorResultHeader title={`${resourceSingular(resourceView)} metadata`} subtitle={resourceSecondary(resourceView, item)} />
      <div className="mb-3 min-h-0 overflow-hidden">
        <DarkSummaryGrid rows={rows.map(([label, value]) => ({ label, value }))} />
      </div>
      <RawDataSection
        title={`${resourceSingular(resourceView)} raw data`}
        value={rawValue}
        search={search}
        onSearch={onSearch}
        inputClass={inputClass}
      />
    </div>
  );
}

function resourceDetailRows(kind: DockerResourceKind, item: DockerResource = {}): [string, unknown][] {
  if (kind === "images") {
    const rows: [string, unknown][] = [
      ["Repository", item.repository],
      ["Tag", item.tag],
      ["Image ID", item.id],
      ["Digest", item.digest],
      ["Size", item.size],
      ["Created", item.created_since || item.created_at],
      ["Visible containers", item.containers ?? 0],
    ];
    return visibleSummaryRows(rows);
  }
  if (kind === "networks") {
    const rows: [string, unknown][] = [
      ["Name", item.name],
      ["Network ID", item.id],
      ["Driver", item.driver],
      ["Scope", item.scope],
      ["IPv6", item.ipv6],
      ["Internal", item.internal],
      ["Visible containers", item.containers ?? 0],
      ["Labels", item.labels],
    ];
    return visibleSummaryRows(rows);
  }
  if (kind === "volumes") {
    const rows: [string, unknown][] = [
      ["Name", item.name],
      ["Driver", item.driver],
      ["Scope", item.scope],
      ["Mountpoint", item.mountpoint],
      ["Visible containers", item.containers ?? 0],
      ["Labels", item.labels],
    ];
    return visibleSummaryRows(rows);
  }
  return Object.entries(item).map(([key, value]) => [key, typeof value === "string" ? value : JSON.stringify(value)]);
}
