import { RefreshCcw } from "lucide-react";
import { connectorActionRiskLabel, connectorActionRiskTone } from "../../lib/connector-action-risks";
import { permissionLifetimeLabel, type PermissionValue } from "../../lib/permissions";
import type { ExecutionRule } from "../../lib/gateway-contracts/security-contracts";
import type { ConnectorPermissionAction } from "../../lib/use-connector-permissions";
import type { PermissionMode } from "./connector-token-permission-model";
import type { PermissionMutationError as MutationFailure, PermissionTarget } from "./use-connector-token-permission-state";
import { ConnectorRuleButton, ConnectorRuleButtons } from "../connectors/connector-rule-button";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Select } from "../ui/form";
import { Notice } from "../ui/notice";

type RuleProps = { rule: string; saving: boolean; disabled?: boolean; onSetRule: (_rule: ExecutionRule | "") => unknown };

export function PermissionMutationError({ value, onRetry }: { value: MutationFailure | null; onRetry: () => unknown }) {
  if (!value) return null;
  return (
    <div role="alert">
      <Notice tone="bad" className="grid gap-2">
        <p>{value.message}</p>
        {value.retryable !== false ? (
          <Button type="button" variant="outline" className="h-8 justify-self-start" onClick={onRetry}>
            <RefreshCcw className="h-3.5 w-3.5" />
            Retry
          </Button>
        ) : null}
      </Notice>
    </div>
  );
}

export function ProjectVisibilityControl({
  projectName,
  enabled,
  ready,
  saving,
  onChange,
}: {
  projectName: string;
  enabled: boolean;
  ready: boolean;
  saving: boolean;
  onChange: (_enabled: boolean) => unknown;
}) {
  return (
    <div
      className={`flex items-center justify-between gap-3 rounded-md border px-3 py-2 text-xs ${enabled ? "border-emerald-200 bg-emerald-50" : "border-amber-200 bg-amber-50"}`}
    >
      <div className="min-w-0">
        <p className="truncate font-semibold text-stone-800">{projectName}</p>
        <p className="truncate text-stone-500">
          {enabled ? "Visible to this token through MCP" : "Hidden from this token's MCP target list"}
        </p>
      </div>
      <Button
        type="button"
        variant="outline"
        className="h-8 shrink-0 px-2 text-xs"
        disabled={saving || !ready}
        onClick={() => onChange(!enabled)}
      >
        {!ready ? "Loading..." : saving ? "Saving..." : enabled ? "Hide" : "Enable"}
      </Button>
    </div>
  );
}

export function ProfileSelect({
  profiles,
  value,
  onChange,
  disabled = false,
}: {
  profiles: PermissionTarget[];
  value?: number;
  onChange: (_id: string) => unknown;
  disabled?: boolean;
}) {
  if (profiles.length === 0) return null;
  return (
    <label className="grid gap-1 text-xs font-semibold text-stone-600">
      Profile
      <Select value={value ? String(value) : ""} disabled={disabled} onChange={(event) => onChange(event.target.value)}>
        <option value="">Select profile</option>
        {profiles.map((profile) => (
          <option key={profile.profile_id} value={profile.profile_id}>
            {profile.profile_label || `Profile ${profile.profile_id}`}
          </option>
        ))}
      </Select>
    </label>
  );
}

export function ProfileLifetimeControls({
  value,
  saving,
  disabled,
  onSetPermanent,
  onSetTemporary,
}: {
  value: PermissionValue;
  saving: boolean;
  disabled: boolean;
  onSetPermanent: () => unknown;
  onSetTemporary: (_lifetime: string) => unknown;
}) {
  const controlsDisabled = saving || disabled;
  const expiresAt = typeof value === "object" ? value?.expires_at : undefined;
  return (
    <div className="dark-panel-subtle grid gap-2 rounded-md border border-stone-200 bg-white/70 p-2 text-xs">
      <div className="flex items-center justify-between gap-2">
        <span className="font-semibold text-stone-700">Lifetime</span>
        <span className="text-stone-500">{permissionLifetimeLabel(value)}</span>
      </div>
      <div className="grid grid-cols-4 gap-1">
        <ConnectorRuleButton active={!disabled && !expiresAt} disabled={controlsDisabled} onClick={onSetPermanent}>
          Keep
        </ConnectorRuleButton>
        {["1h", "4h", "1d"].map((lifetime) => (
          <ConnectorRuleButton key={lifetime} active={false} disabled={controlsDisabled} onClick={() => onSetTemporary(lifetime)}>
            {lifetime}
          </ConnectorRuleButton>
        ))}
      </div>
    </div>
  );
}

const modes = [
  { id: "basic", label: "Basic", title: "Apply one rule to every connector action." },
  { id: "grouped", label: "Grouped", title: "Apply separate rules to read and write actions." },
  { id: "advanced", label: "Advanced", title: "Configure every connector action separately." },
] as const;

export function PermissionModeTabs({
  value,
  onChange,
  disabled = false,
}: {
  value: PermissionMode;
  onChange: (_mode: PermissionMode) => unknown;
  disabled?: boolean;
}) {
  return (
    <div className="grid grid-cols-3 gap-1 rounded-md border border-stone-200 bg-white/70 p-1 dark-panel-subtle">
      {modes.map((mode) => (
        <button
          key={mode.id}
          type="button"
          disabled={disabled}
          title={mode.title}
          className={`h-8 rounded px-2 text-xs font-semibold transition ${value === mode.id ? "permission-button-active bg-emerald-950 text-white" : "text-stone-600 hover:bg-stone-100"}`}
          onClick={() => onChange(mode.id)}
        >
          {mode.label}
        </button>
      ))}
    </div>
  );
}

export function PermissionRuleGroup({
  title,
  description,
  rule,
  saving,
  disabled = false,
  onSetRule,
}: RuleProps & { title: string; description: string }) {
  return (
    <div
      role="group"
      aria-label={`${title} permission`}
      className="dark-panel-subtle grid gap-2 rounded-md border border-stone-200 bg-white/70 p-2"
    >
      <div className="flex min-w-0 items-center justify-between gap-2">
        <div className="min-w-0">
          <p className="truncate text-xs font-semibold text-stone-900">{title}</p>
          <p className="truncate text-xs text-stone-500">{description}</p>
        </div>
        {rule === "mixed" ? <Badge tone="warn">mixed</Badge> : null}
      </div>
      <ConnectorRuleButtons rule={rule} saving={saving} disabled={disabled} onSetRule={onSetRule} />
    </div>
  );
}

export function ActionPermissionCard({
  action,
  rule,
  saving,
  compactPopover,
  onSetRule,
}: RuleProps & { action: ConnectorPermissionAction; compactPopover: boolean }) {
  return (
    <div
      role="group"
      aria-label={`${action.name} permission`}
      className={`grid gap-2 rounded-md border border-stone-200 bg-white/70 p-2 ${compactPopover ? "" : "dark-panel-subtle"}`}
    >
      <div className="flex min-w-0 items-center justify-between gap-2">
        <div className="min-w-0">
          <p className="truncate font-mono text-xs font-semibold text-stone-900">{action.name}</p>
          <p className="line-clamp-2 text-xs text-stone-500">{action.description}</p>
        </div>
        <Badge tone={connectorActionRiskTone(action.risk)}>{connectorActionRiskLabel(action.risk)}</Badge>
      </div>
      <ConnectorRuleButtons rule={rule} saving={saving} onSetRule={onSetRule} />
    </div>
  );
}
