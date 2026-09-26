import { KeyRound, PanelRightClose, PanelRightOpen, RefreshCcw, TicketCheck } from "lucide-react";
import {
  connectorTargetProfileLifetime,
  currentConnectorTargetProfilePermissions,
  matchesConnectorTargetProfileAction,
  selectedConnectorProfile,
} from "../../lib/connector-permissions";
import { connectorActionCacheKey } from "../../lib/use-connector-permissions";
import { effectiveRule, expiresAtFromLifetime, maskedToken, ruleLabel } from "../../lib/permissions";
import { Badge, CountBadge } from "../ui/badge";
import { Button } from "../ui/button";
import { Notice } from "../ui/notice";
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
import type { ConnectorTokenPermissionOptions, PermissionTarget } from "./use-connector-token-permission-state";
import type { GatewayToken } from "../../lib/gateway-contracts/core-resource-contracts";
import type { RuntimeMessage } from "../../lib/gateway-contracts/activity-resource-contracts";
import type { TokenActionPermission } from "../../lib/gateway-contracts/security-contracts";
import {
  ActionPermissionCard,
  PermissionMutationError,
  PermissionModeTabs,
  PermissionRuleGroup,
  ProfileLifetimeControls,
  ProfileSelect,
  ProjectVisibilityControl,
} from "./connector-token-permission-controls";

export type ConnectorTokenPermissionPanelProps = Omit<ConnectorTokenPermissionOptions, "tokens"> & {
  tokens: ConnectorTokenPermissionOptions["tokens"] & { state: string; error?: string | null };
  unreadMessages?: Pick<RuntimeMessage, "runtime_id" | "token_id">[];
  compact?: boolean;
  onToggleCompact?: () => void;
  onOpenMessages?: (_id: number) => void;
};
type Panel = ReturnType<typeof useConnectorTokenPermissionState>;
type ActionProps = {
  panel: Panel;
  selectedTarget: PermissionTarget | null;
  token: GatewayToken;
  profile: PermissionTarget;
  compactPopover?: boolean;
};
type RuleGroupProps = {
  groups: ReturnType<typeof groupActions>;
  permissions: TokenActionPermission[];
  profile: PermissionTarget;
  saving: boolean;
  selectedTarget: PermissionTarget;
  token: GatewayToken;
};

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
}: ConnectorTokenPermissionPanelProps) {
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
    savingKey,
    selectProfile,
    selectedCountByToken,
    setOpenTokenID,
    tokenTriggerRef,
    targetProfiles,
  } = panel;

  if (compact) {
    return (
      <aside
        ref={compactPanelRef}
        className="relative grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-visible rounded-lg border border-stone-200 bg-white"
      >
        <header className="grid gap-2 border-b border-stone-200 p-2">
          <Button type="button" variant="ghost" className="h-9 w-9 px-0" title="Expand tokens" onClick={onToggleCompact}>
            <PanelRightOpen className="h-4 w-4" />
          </Button>
          <Button
            type="button"
            variant="outline"
            className="h-9 w-9 px-0"
            title="Refresh connector permissions"
            onClick={refreshPanel}
            disabled={Boolean(savingKey)}
          >
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
                  title={selectedTarget ? `${token.name}: ${selectedCount} connector grants` : "Select a connector first"}
                  disabled={!selectedTarget}
                  aria-expanded={open}
                  aria-controls={`connector-token-popover-${token.id}`}
                  onClick={(event) => {
                    tokenTriggerRef.current = event.currentTarget;
                    setOpenTokenID(open ? null : token.id);
                  }}
                >
                  <KeyRound className="h-4 w-4" />
                  {selectedCount > 0 ? <CountBadge className="absolute -right-1 -top-1">{selectedCount}</CountBadge> : null}
                </button>
                {open ? (
                  <div
                    id={`connector-token-popover-${token.id}`}
                    className="absolute right-full top-0 z-30 mr-2 grid max-h-[70vh] w-96 gap-3 overflow-auto rounded-lg border border-stone-200 bg-white p-3 shadow-xl"
                  >
                    <div className="min-w-0">
                      <p className="truncate text-sm font-semibold text-stone-900">{token.name}</p>
                      <p className="mt-1 text-xs text-stone-500">{selectedTarget?.target_name || "Select a connector"}</p>
                    </div>
                    <ProfileSelect
                      profiles={targetProfiles}
                      value={profile?.profile_id}
                      onChange={(profileID) => selectProfile(token, profileID)}
                      disabled={Boolean(panel.savingKey)}
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
    <aside className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border border-stone-200 bg-white">
      <header className="flex items-center justify-between gap-3 border-b border-stone-200 px-4 py-3">
        <div className="min-w-0">
          <h3 className="flex items-center gap-2 text-sm font-semibold">
            <TicketCheck className="h-4 w-4" />
            Tokens
          </h3>
          <p className="mt-1 truncate text-xs text-stone-500">{selectedTarget ? selectedTarget.target_name : "Select a connector"}</p>
        </div>
        <div className="flex gap-2">
          {onToggleCompact ? (
            <Button type="button" variant="ghost" className="h-9 w-9 px-0" title="Collapse tokens" onClick={onToggleCompact}>
              <PanelRightClose className="h-4 w-4" />
            </Button>
          ) : null}
          <Button
            type="button"
            variant="outline"
            className="h-9 w-9 px-0"
            title="Refresh connector permissions"
            onClick={refreshPanel}
            disabled={load.state === "loading" || Boolean(savingKey)}
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
                    Number(message.runtime_id) === Number(profile?.runtime_id || selectedTarget?.runtime_id) &&
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
                  disabled={Boolean(panel.savingKey)}
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

function TokenPermissionActions({ panel, selectedTarget, token, profile, compactPopover = false }: ActionProps) {
  const {
    load,
    permissionModeByKey,
    permissionMutationError,
    permissionsByToken,
    projectEnabledForToken,
    projectScopeReadyForToken,
    retryPermissionMutation,
    savingKey,
    selectedTargetKey,
    setConnectorRule,
    setConnectorRules,
    setPermissionModeByKey,
    setProfileLifetime,
    setProjectVisibility,
  } = panel;
  if (!selectedTarget) return null;
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
        ready={projectScopeReadyForToken(token.id)}
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
          disabled={saving}
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

function GroupedPermissionRules({
  groups,
  permissions,
  profile,
  saving,
  selectedTarget,
  setConnectorRules,
  token,
}: Omit<RuleGroupProps, "groups"> & { groups: ReturnType<typeof groupActionsByRisk>; setConnectorRules: Panel["setConnectorRules"] }) {
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

function AdvancedPermissionRules({
  compactPopover,
  groups,
  permissions,
  profile,
  saving,
  selectedTarget,
  setConnectorRule,
  token,
}: RuleGroupProps & { compactPopover: boolean; setConnectorRule: Panel["setConnectorRule"] }) {
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
