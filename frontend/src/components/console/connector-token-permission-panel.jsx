import { KeyRound, PanelRightClose, PanelRightOpen, RefreshCcw, TicketCheck } from "lucide-react";
import {
  connectorTargetProfileLifetime,
  currentConnectorTargetProfilePermissions,
  matchesConnectorTargetProfileAction,
  selectedConnectorProfile,
} from "../../lib/connector-permissions";
import { connectorActionRiskLabel, connectorActionRiskTone } from "../../lib/connector-action-risks";
import { connectorActionCacheKey } from "../../lib/use-connector-permissions";
import { effectiveRule, expiresAtFromLifetime, maskedToken, permissionLifetimeLabel, ruleLabel } from "../../lib/permissions";
import { Badge, CountBadge } from "../ui/badge";
import { Button } from "../ui/button";
import { Select } from "../ui/form";
import { Notice } from "../ui/notice";
import { ConnectorRuleButton } from "../connectors/connector-rule-button";
import {
  groupActions,
  groupActionsByRisk,
  inferPermissionMode,
  matchesPermissionMutationError,
  ruleForActions,
  targetSupportsMessages,
  tokenProfileModeKey,
} from "./connector-token-permission-model";
import { useConnectorTokenPermissionState } from "./use-connector-token-permission-state";

export function ConnectorTokenPermissionPanel({
  tokens,
  selectedTarget,
  targets,
  unreadMessages = [],
  compact = false,
  connectorPermissionState,
  loadAllConnectorPermissions,
  loadConnectorActions,
  replaceTokenConnectorPermissions,
  onToggleCompact,
  onRefresh,
  onOpenMessages,
}) {
  const panel = useConnectorTokenPermissionState({
    connectorPermissionState,
    loadAllConnectorPermissions,
    loadConnectorActions,
    onRefresh,
    replaceTokenConnectorPermissions,
    selectedTarget,
    targets,
    tokens,
  });
  const {
    activeTokens,
    compactPanelRef,
    load,
    openTokenID,
    profileByToken,
    projectScopeError,
    refreshPanel,
    selectProfile,
    selectedCountByToken,
    setOpenTokenID,
    targetProfiles,
  } = panel;

  if (compact) {
    return (
      <aside
        ref={compactPanelRef}
        className="relative grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-visible rounded-lg border border-stone-200 bg-white"
      >
        <header className="grid gap-2 border-b border-stone-200 p-2">
          <Button type="button" variant="ghost" className="h-9 w-9 px-0" title="Expand tokens" onClick={onToggleCompact}>
            <PanelRightOpen className="h-4 w-4" />
          </Button>
          <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Refresh connector permissions" onClick={refreshPanel}>
            <RefreshCcw className="h-4 w-4" />
          </Button>
        </header>
        <div className="grid content-start gap-2 overflow-visible p-2">
          {activeTokens.map((token) => {
            const open = Number(openTokenID) === Number(token.id);
            const profile = selectedConnectorProfile(token.id, selectedTarget, targetProfiles, profileByToken);
            const selectedCount = selectedCountByToken[token.id] || 0;
            return (
              <div className="relative" key={token.id}>
                <button
                  type="button"
                  className={`relative grid h-10 w-10 place-items-center rounded-md border text-stone-700 transition hover:bg-stone-100 ${selectedCount > 0 ? "border-emerald-700" : "border-stone-300"}`}
                  title={`${token.name}: ${selectedCount} connector grants`}
                  onClick={() => setOpenTokenID(open ? null : token.id)}
                >
                  <KeyRound className="h-4 w-4" />
                  {selectedCount > 0 ? <CountBadge className="absolute -right-1 -top-1">{selectedCount}</CountBadge> : null}
                </button>
                {open ? (
                  <div className="absolute right-full top-0 z-30 mr-2 grid max-h-[70vh] w-96 gap-3 overflow-auto rounded-lg border border-stone-200 bg-white p-3 shadow-xl">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-semibold text-stone-900">{token.name}</p>
                      <p className="mt-1 text-xs text-stone-500">{selectedTarget.target_name}</p>
                    </div>
                    <ProfileSelect
                      profiles={targetProfiles}
                      value={profile?.profile_id}
                      onChange={(profileID) => selectProfile(token, profileID)}
                    />
                    {profile ? (
                      <TokenPermissionActions
                        panel={panel}
                        selectedTarget={selectedTarget}
                        token={token}
                        profile={profile}
                        compactPopover
                      />
                    ) : (
                      <Notice>No credential profiles for this connector.</Notice>
                    )}
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
      </aside>
    );
  }

  return (
    <aside className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border border-stone-200 bg-white">
      <header className="flex items-center justify-between gap-3 border-b border-stone-200 px-4 py-3">
        <div className="min-w-0">
          <h3 className="flex items-center gap-2 text-sm font-semibold">
            <TicketCheck className="h-4 w-4" />
            Tokens
          </h3>
          <p className="mt-1 truncate text-xs text-stone-500">{selectedTarget ? selectedTarget.target_name : "Select a connector"}</p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="ghost" className="h-9 w-9 px-0" title="Collapse tokens" onClick={onToggleCompact}>
            <PanelRightClose className="h-4 w-4" />
          </Button>
          <Button
            type="button"
            variant="outline"
            className="h-9 w-9 px-0"
            title="Refresh connector permissions"
            onClick={refreshPanel}
            disabled={load.state === "loading"}
          >
            <RefreshCcw className="h-4 w-4" />
          </Button>
        </div>
      </header>

      <div className="min-h-0 overflow-auto p-3">
        {load.state === "loading" ? <Notice>Loading connector permissions...</Notice> : null}
        {load.state === "error" ? <Notice tone="bad">{load.error}</Notice> : null}
        {projectScopeError ? <Notice tone="bad">{projectScopeError}</Notice> : null}
        {tokens.state === "error" ? <Notice tone="bad">{tokens.error}</Notice> : null}
        {tokens.state === "ready" && tokens.data.length === 0 ? <Notice>Create a token first.</Notice> : null}
        {tokens.state === "ready" && tokens.data.length > 0 && activeTokens.length === 0 ? <Notice>No active tokens.</Notice> : null}

        <div className="mt-3 grid gap-3">
          {activeTokens.map((token) => {
            const selectedCount = selectedCountByToken[token.id] || 0;
            const profile = selectedConnectorProfile(token.id, selectedTarget, targetProfiles, profileByToken);
            const unreadCount = targetSupportsMessages(selectedTarget)
              ? unreadMessages.filter(
                  (message) =>
                    Number(message.runtime_id) === Number(profile?.runtime_id || selectedTarget.runtime_id) &&
                    Number(message.token_id) === Number(token.id),
                ).length
              : 0;
            return (
              <section
                className={`grid gap-3 rounded-lg border p-3 transition ${selectedCount > 0 ? "border-emerald-200 bg-emerald-50" : "border-stone-200 bg-white"}`}
                key={token.id}
              >
                <div className="flex min-w-0 items-start justify-between gap-3">
                  <div className="min-w-0">
                    <button
                      type="button"
                      className={`flex max-w-full min-w-0 items-center gap-2 text-left text-sm font-semibold ${
                        unreadCount > 0 ? "cursor-pointer hover:text-emerald-700" : "cursor-default"
                      }`}
                      onClick={() => unreadCount > 0 && onOpenMessages?.(token.id)}
                    >
                      <KeyRound className="h-4 w-4 shrink-0 text-stone-500" />
                      <span className="truncate">{token.name}</span>
                      {unreadCount > 0 ? <CountBadge>{unreadCount}</CountBadge> : null}
                    </button>
                    <p className="mt-1 truncate font-mono text-[11px] text-stone-500">{maskedToken(token.token)}</p>
                  </div>
                  <Badge tone={selectedCount > 0 ? "good" : "neutral"}>
                    {selectedCount > 0 ? `${selectedCount} grants` : ruleLabel("")}
                  </Badge>
                </div>
                <ProfileSelect
                  profiles={targetProfiles}
                  value={profile?.profile_id}
                  onChange={(profileID) => selectProfile(token, profileID)}
                />
                {profile ? (
                  <TokenPermissionActions panel={panel} selectedTarget={selectedTarget} token={token} profile={profile} />
                ) : (
                  <Notice>No credential profiles for this connector.</Notice>
                )}
              </section>
            );
          })}
        </div>
      </div>
    </aside>
  );
}

function TokenPermissionActions({ panel, selectedTarget, token, profile, compactPopover = false }) {
  const {
    load,
    permissionModeByKey,
    permissionMutationError,
    permissionsByToken,
    projectEnabledForToken,
    retryPermissionMutation,
    savingKey,
    selectedTargetKey,
    setConnectorRule,
    setConnectorRules,
    setPermissionModeByKey,
    setProfileLifetime,
    setProjectVisibility,
  } = panel;
  const permissions = permissionsByToken[token.id] || [];
  const actions = load.actionsByTargetRef?.[connectorActionCacheKey(selectedTarget, profile.profile_id)] || [];
  const activePermissions = currentConnectorTargetProfilePermissions(permissions, selectedTarget, profile.profile_id);
  const lifetimeValue = connectorTargetProfileLifetime(permissions, selectedTarget, profile.profile_id);
  const lifetimeEditable = activePermissions.some((permission) => effectiveRule(permission) !== "blocked");
  const categoryGroups = groupActions(actions);
  const riskGroups = groupActionsByRisk(actions);
  const modeKey = tokenProfileModeKey(token.id, selectedTarget, profile.profile_id);
  const permissionMode = permissionModeByKey[modeKey] || inferPermissionMode(permissions, selectedTarget, profile.profile_id, actions);
  const saving = Boolean(savingKey);

  return (
    <div className="grid gap-2">
      {matchesPermissionMutationError(permissionMutationError, token.id, profile.profile_id, selectedTargetKey) ? (
        <PermissionMutationError value={permissionMutationError} onRetry={retryPermissionMutation} />
      ) : null}
      <ProjectVisibilityControl
        projectName={selectedTarget.project_name || "Ungrouped"}
        enabled={projectEnabledForToken(token.id)}
        saving={saving}
        onChange={(enabled) => setProjectVisibility(token, enabled)}
      />
      <ProfileLifetimeControls
        value={lifetimeValue}
        saving={saving}
        disabled={!lifetimeEditable}
        onSetPermanent={() => setProfileLifetime(token, profile.profile_id, "")}
        onSetTemporary={(lifetime) => setProfileLifetime(token, profile.profile_id, expiresAtFromLifetime(lifetime))}
      />
      {actions.length > 0 ? (
        <PermissionModeTabs
          value={permissionMode}
          onChange={(mode) => setPermissionModeByKey((current) => ({ ...current, [modeKey]: mode }))}
        />
      ) : null}
      {permissionMode === "basic" && actions.length > 0 ? (
        <PermissionRuleGroup
          title="All operations"
          description={`${actions.length} connector action${actions.length === 1 ? "" : "s"}`}
          rule={ruleForActions(permissions, selectedTarget, profile.profile_id, actions)}
          saving={saving}
          onSetRule={(rule) => setConnectorRules(token, profile.profile_id, actions, rule, "all")}
        />
      ) : null}
      {permissionMode === "grouped" && actions.length > 0 ? (
        <GroupedPermissionRules
          groups={riskGroups}
          permissions={permissions}
          profile={profile}
          saving={saving}
          selectedTarget={selectedTarget}
          setConnectorRules={setConnectorRules}
          token={token}
        />
      ) : null}
      {permissionMode === "advanced" ? (
        <AdvancedPermissionRules
          compactPopover={compactPopover}
          groups={categoryGroups}
          permissions={permissions}
          profile={profile}
          saving={saving}
          selectedTarget={selectedTarget}
          setConnectorRule={setConnectorRule}
          token={token}
        />
      ) : null}
      {load.state === "ready" && actions.length === 0 ? <Notice>No actions exposed by this connector.</Notice> : null}
    </div>
  );
}

function GroupedPermissionRules({ groups, permissions, profile, saving, selectedTarget, setConnectorRules, token }) {
  return (
    <div className="grid gap-2">
      {groups.map((group) => (
        <PermissionRuleGroup
          key={group.key}
          title={group.name}
          description={group.description}
          rule={ruleForActions(permissions, selectedTarget, profile.profile_id, group.actions)}
          saving={saving}
          disabled={group.actions.length === 0}
          onSetRule={(rule) => setConnectorRules(token, profile.profile_id, group.actions, rule, group.key)}
        />
      ))}
    </div>
  );
}

function AdvancedPermissionRules({ compactPopover, groups, permissions, profile, saving, selectedTarget, setConnectorRule, token }) {
  return groups.map((group) => (
    <div key={group.name} className="grid gap-2">
      {groups.length > 1 ? <p className="text-[11px] font-semibold uppercase tracking-wide text-stone-500">{group.name}</p> : null}
      {group.actions.map((action) => {
        const permission = permissions.find((item) =>
          matchesConnectorTargetProfileAction(item, selectedTarget, profile.profile_id, action.name),
        );
        return (
          <ActionPermissionCard
            key={action.name}
            action={action}
            rule={effectiveRule(permission) || ""}
            saving={saving}
            compactPopover={compactPopover}
            onSetRule={(rule) => setConnectorRule(token, profile.profile_id, action, rule)}
          />
        );
      })}
    </div>
  ));
}

function PermissionMutationError({ value, onRetry }) {
  return (
    <div role="alert">
      <Notice tone="bad" className="grid gap-2">
        <p>{value.message}</p>
        <Button type="button" variant="outline" className="h-8 justify-self-start" onClick={onRetry}>
          <RefreshCcw className="h-3.5 w-3.5" />
          Retry
        </Button>
      </Notice>
    </div>
  );
}

function ProjectVisibilityControl({ projectName, enabled, saving, onChange }) {
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
      <Button type="button" variant="outline" className="h-8 shrink-0 px-2 text-xs" disabled={saving} onClick={() => onChange(!enabled)}>
        {saving ? "Saving..." : enabled ? "Hide" : "Enable"}
      </Button>
    </div>
  );
}

function ProfileSelect({ profiles, value, onChange }) {
  if (profiles.length === 0) return null;
  return (
    <label className="grid gap-1 text-xs font-semibold text-stone-600">
      Profile
      <Select value={value ? String(value) : ""} onChange={(event) => onChange(event.target.value)}>
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

function ProfileLifetimeControls({ value, saving, disabled, onSetPermanent, onSetTemporary }) {
  const controlsDisabled = saving || disabled;
  return (
    <div className="dark-panel-subtle grid gap-2 rounded-md border border-stone-200 bg-white/70 p-2 text-xs">
      <div className="flex items-center justify-between gap-2">
        <span className="font-semibold text-stone-700">Lifetime</span>
        <span className="text-stone-500">{permissionLifetimeLabel(value)}</span>
      </div>
      <div className="grid grid-cols-4 gap-1">
        <ConnectorRuleButton active={!disabled && !value?.expires_at} disabled={controlsDisabled} onClick={onSetPermanent}>
          Keep
        </ConnectorRuleButton>
        <ConnectorRuleButton active={false} disabled={controlsDisabled} onClick={() => onSetTemporary("1h")}>
          1h
        </ConnectorRuleButton>
        <ConnectorRuleButton active={false} disabled={controlsDisabled} onClick={() => onSetTemporary("4h")}>
          4h
        </ConnectorRuleButton>
        <ConnectorRuleButton active={false} disabled={controlsDisabled} onClick={() => onSetTemporary("1d")}>
          1d
        </ConnectorRuleButton>
      </div>
    </div>
  );
}

function PermissionModeTabs({ value, onChange }) {
  const modes = [
    { id: "basic", label: "Basic", title: "Apply one rule to every connector action." },
    { id: "grouped", label: "Grouped", title: "Apply separate rules to read and write actions." },
    { id: "advanced", label: "Advanced", title: "Configure every connector action separately." },
  ];
  return (
    <div className="grid grid-cols-3 gap-1 rounded-md border border-stone-200 bg-white/70 p-1 dark-panel-subtle">
      {modes.map((mode) => (
        <button
          key={mode.id}
          type="button"
          title={mode.title}
          className={`h-8 rounded px-2 text-xs font-semibold transition ${
            value === mode.id ? "permission-button-active bg-emerald-950 text-white" : "text-stone-600 hover:bg-stone-100"
          }`}
          onClick={() => onChange(mode.id)}
        >
          {mode.label}
        </button>
      ))}
    </div>
  );
}

function PermissionRuleGroup({ title, description, rule, saving, disabled = false, onSetRule }) {
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
      <div className="grid grid-cols-4 gap-1">
        <ConnectorRuleButton active={!rule && !disabled} disabled={saving || disabled} onClick={() => onSetRule("")}>
          Disabled
        </ConnectorRuleButton>
        <ConnectorRuleButton active={rule === "blocked"} disabled={saving || disabled} onClick={() => onSetRule("blocked")}>
          Blocked
        </ConnectorRuleButton>
        <ConnectorRuleButton
          active={rule === "approval_required"}
          disabled={saving || disabled}
          onClick={() => onSetRule("approval_required")}
        >
          Prompt
        </ConnectorRuleButton>
        <ConnectorRuleButton active={rule === "always_run"} disabled={saving || disabled} onClick={() => onSetRule("always_run")}>
          Always
        </ConnectorRuleButton>
      </div>
    </div>
  );
}

function ActionPermissionCard({ action, rule, saving, compactPopover, onSetRule }) {
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
      <div className="grid grid-cols-4 gap-1">
        <ConnectorRuleButton active={!rule} disabled={saving} onClick={() => onSetRule("")}>
          Disabled
        </ConnectorRuleButton>
        <ConnectorRuleButton active={rule === "blocked"} disabled={saving} onClick={() => onSetRule("blocked")}>
          Blocked
        </ConnectorRuleButton>
        <ConnectorRuleButton active={rule === "approval_required"} disabled={saving} onClick={() => onSetRule("approval_required")}>
          Prompt
        </ConnectorRuleButton>
        <ConnectorRuleButton active={rule === "always_run"} disabled={saving} onClick={() => onSetRule("always_run")}>
          Always
        </ConnectorRuleButton>
      </div>
    </div>
  );
}
