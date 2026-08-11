import { FileJson, LoaderCircle } from "lucide-react";
import { CopyButton } from "../../../components/ui/copy-button";
import { Input } from "../../../components/ui/form";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { resourceSubtitle } from "./helpers";

export function KubernetesResourceDetail({ tab, resource, detail, logs, search, onSearch, inputClass, mutedClass }) {
  if (!resource) {
    return (
      <div className={`grid h-full min-h-0 place-items-center rounded-lg border border-dashed p-8 text-center text-sm ${mutedClass}`}>
        Select a Kubernetes resource to inspect metadata, logs, and raw JSON.
      </div>
    );
  }
  const rawResource = detail?.output?.resource || detail?.output || resource;
  const rawValue = JSON.stringify(rawResource || {}, null, 2);
  const topTitle = kubernetesTopTitle(tab);
  const topSubtitle = kubernetesTopSubtitle(tab, resource);
  const topCopyValue = tab === "pods" ? logs : kubernetesMetadataText(tab, resource);
  const showLogSurface = tab === "pods";
  return (
    <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,600px)_auto_minmax(0,1fr)] overflow-hidden">
      <KubernetesResultHeader
        title={topTitle}
        subtitle={topSubtitle}
        copyValue={topCopyValue}
        search={search}
        onSearch={onSearch}
        inputClass={inputClass}
        searchPlaceholder={tab === "pods" ? "Search logs" : "Search metadata"}
      />
      {showLogSurface ? (
        <TerminalBlock
          className="h-full min-h-0 max-h-full overflow-auto whitespace-pre text-xs"
          surface="log"
          style={{ whiteSpace: "pre", overflowWrap: "normal", wordBreak: "normal" }}
        >
          <HighlightedText text={logs || "Click Logs to load bounded pod logs for this pod."} query={search} />
        </TerminalBlock>
      ) : (
        <div className="min-h-0 overflow-auto rounded-md border border-stone-700 bg-[#1a1a1a] p-3">
          <KubernetesSummaryCards tab={tab} resource={resource} mutedClass={mutedClass} />
          {tab === "nodes" ? (
            <p className="mt-3 rounded-md border border-amber-700/50 bg-amber-950/30 p-3 text-xs text-amber-100">
              Kubernetes nodes do not expose pod-style logs through kubectl logs. Use metadata, conditions, and events for node
              investigation.
            </p>
          ) : null}
          {tab === "events" && resource.message ? (
            <TerminalBlock className="mt-3 min-h-32 text-xs" surface="dark">
              <HighlightedText text={resource.message} query={search} />
            </TerminalBlock>
          ) : null}
        </div>
      )}
      <div className="mt-3 flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <FileJson className="h-3.5 w-3.5 text-stone-500" />
          <p className="truncate text-xs font-semibold uppercase tracking-wide text-stone-500">Kubernetes raw data</p>
        </div>
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
      <div className="mt-2 grid h-full min-h-0 overflow-hidden">
        <TerminalBlock className="h-full min-h-0 whitespace-pre-wrap break-words text-xs [overflow-wrap:anywhere]" surface="dark">
          <HighlightedText text={rawValue} query={search} />
        </TerminalBlock>
      </div>
    </div>
  );
}

export function KubernetesHeaderStatus({ state, mutedClass }) {
  if (state.state !== "idle" && state.state !== "error") {
    return (
      <p className="mt-1 flex min-h-4 items-center gap-1 truncate text-[11px] text-amber-500">
        <LoaderCircle className="h-3 w-3 shrink-0 animate-spin" />
        <span className="truncate">{state.state}</span>
      </p>
    );
  }
  if (state.state === "error" && state.error) {
    return <p className="mt-1 min-h-4 truncate text-[11px] text-red-500">{state.error}</p>;
  }
  return <p className={`mt-1 min-h-4 text-[11px] ${mutedClass}`}>&nbsp;</p>;
}

function KubernetesResultHeader({ title, subtitle, copyValue, search, onSearch, inputClass, searchPlaceholder }) {
  return (
    <div className="mb-2 flex items-center justify-between gap-3">
      <div className="min-w-0">
        <p className="truncate text-xs font-semibold uppercase tracking-wide text-stone-500">{title}</p>
        {subtitle ? <p className="truncate text-xs text-stone-500">{subtitle}</p> : null}
      </div>
      <div className="flex min-w-0 items-center justify-end gap-2">
        <Input
          className={`h-8 w-56 text-xs ${inputClass || ""}`}
          value={search}
          onChange={(event) => onSearch?.(event.target.value)}
          placeholder={searchPlaceholder || "Search"}
        />
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

function kubernetesTopTitle(tab) {
  if (tab === "pods") return "Pod logs";
  if (tab === "nodes") return "Node metadata";
  if (tab === "events") return "Event details";
  return "Resource metadata";
}

function kubernetesTopSubtitle(tab, resource) {
  if (!resource) return "";
  if (tab === "pods") return `${resource.namespace}/${resource.name}`;
  if (tab === "nodes") return "Node logs are not available through kubectl logs.";
  if (tab === "events") return `${resource.namespace || "-"} · ${resource.object || "-"}`;
  return resourceSubtitle(tab, resource);
}

function kubernetesMetadataText(tab, resource) {
  return summaryRows(tab, resource)
    .map((row) => `${row.label}: ${row.value || "-"}`)
    .join("\n");
}

function KubernetesSummaryCards({ tab, resource, detail, mutedClass }) {
  if (!resource) {
    return <p className={`text-sm ${mutedClass}`}>No resource selected.</p>;
  }
  const rows = summaryRows(tab, resource, detail);
  return (
    <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
      {rows.map((row) => (
        <div key={row.label} className="rounded-md border border-stone-700/40 p-2">
          <p className={`text-[11px] uppercase ${mutedClass}`}>{row.label}</p>
          <p className="truncate text-sm font-semibold" title={String(row.value || "-")}>
            {row.value || "-"}
          </p>
        </div>
      ))}
    </div>
  );
}

export function KubernetesFooter({ target, borderClass, mutedClass }) {
  return (
    <div className={`flex min-h-9 items-center justify-between border-t px-4 py-2 text-xs ${borderClass} ${mutedClass}`}>
      <span>Kubernetes transport</span>
      <span className="font-mono">{target.config?.transport_target_ref || "no transport"}</span>
    </div>
  );
}

function summaryRows(tab, resource) {
  if (tab === "workloads")
    return [
      { label: "Kind", value: resource.kind },
      { label: "Namespace", value: resource.namespace },
      { label: "Ready", value: resource.ready },
      { label: "Image", value: resource.image },
    ];
  if (tab === "pods")
    return [
      { label: "Namespace", value: resource.namespace },
      { label: "Phase", value: resource.phase },
      { label: "Ready", value: resource.ready },
      { label: "Restarts", value: resource.restarts },
    ];
  if (tab === "services")
    return [
      { label: "Namespace", value: resource.namespace },
      { label: "Type", value: resource.type },
      { label: "Cluster IP", value: resource.cluster_ip },
      { label: "Ports", value: resource.ports },
    ];
  if (tab === "ingress")
    return [
      { label: "Namespace", value: resource.namespace },
      { label: "Class", value: resource.class },
      { label: "Hosts", value: resource.hosts },
      { label: "Age", value: resource.age },
    ];
  if (tab === "nodes")
    return [
      { label: "Name", value: resource.name },
      { label: "Ready", value: resource.ready },
      { label: "Roles", value: resource.roles },
      { label: "Version", value: resource.version },
    ];
  return [
    { label: "Type", value: resource.type },
    { label: "Reason", value: resource.reason },
    { label: "Object", value: resource.object },
    { label: "Count", value: resource.count },
  ];
}
