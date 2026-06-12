import { Database, KeyRound, Server } from "lucide-react";
import { Badge } from "../../components/ui/badge";
import { CopyButton } from "../../components/ui/copy-button";
import { TerminalBlock } from "../../components/ui/terminal-block";
import { connectorBadgeTone, connectorKindLabel, connectorSummary, connectorTemplateMetadata } from "./catalog";

export { connectorBadgeTone, connectorKindLabel, connectorSummary } from "./catalog";

export function ConnectorKindCell({ target, catalog }) {
  return (
    <td className="px-4 py-4">
      <div className="flex min-w-0 items-center gap-2">
        <ConnectorIcon kind={target.connector_kind} className="h-4 w-4 shrink-0 text-stone-500" />
        <div className="grid min-w-0 gap-1">
          <span className="truncate font-semibold">{catalogLabel(catalog, target.connector_kind)}</span>
          <span className="font-mono text-xs text-stone-500">{target.connector_kind}:{target.id}</span>
        </div>
      </div>
    </td>
  );
}

export function TargetCell({ target, endpoint }) {
  return (
    <td className="px-4 py-4">
      <div className="grid min-w-0 gap-1">
        <span className="truncate font-semibold text-stone-900">{target.name}</span>
        <span className="truncate font-mono text-xs text-stone-500">{endpoint}</span>
      </div>
    </td>
  );
}

export function ProfilesCell({ target }) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {(target.profiles || []).map((profile) => (
        <Badge key={profile.id} tone="neutral" title={profile.ref}>
          {profile.label}
        </Badge>
      ))}
      {(target.profiles || []).length === 0 ? <span className="text-xs text-stone-500">No profiles</span> : null}
    </div>
  );
}

export function StatusCell({ target }) {
  return <Badge tone={target.status === "active" ? "good" : "warn"}>{target.status}</Badge>;
}

export function InstallCommandPanel({ command, title, description }) {
  return (
    <div className="grid min-w-0 gap-3 overflow-hidden rounded-lg border border-stone-200 bg-white p-3">
      <div className="flex min-w-0 items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-sm font-semibold text-stone-900">{title}</p>
          <p className="mt-1 text-xs text-stone-500">{description}</p>
        </div>
        <CopyButton value={command} variant="outline" className="h-9 shrink-0 px-3" />
      </div>
      <TerminalBlock className="max-h-44 max-w-full whitespace-pre p-3">{command}</TerminalBlock>
    </div>
  );
}

export function ConnectorIcon({ kind, className = "" }) {
  const icon = connectorIconName(kind);
  if (icon === "server") return <Server className={className} />;
  if (icon === "database") return <Database className={className} />;
  return <KeyRound className={className} />;
}

export function catalogLabel(catalog, kind) {
  return catalog.data.find((item) => item.kind === kind)?.label || connectorKindLabel(kind);
}

function connectorIconName(kind) {
  return connectorTemplateMetadata[kind]?.icon || "key";
}
