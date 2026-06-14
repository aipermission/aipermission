import { KeyRound, PanelRightClose, PanelRightOpen, RefreshCcw, TicketCheck } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import {
  connectorTargetProfileLifetime,
  currentConnectorTargetProfilePermissions,
  matchesConnectorTargetProfile,
  matchesConnectorTargetProfileAction,
  profilesForConnectorTarget,
  readStoredConnectorProfileID,
  selectedConnectorProfile,
  selectedConnectorProfileID,
  writeStoredConnectorProfileID,
} from "../../lib/connector-permissions";
import { connectorActionCacheKey } from "../../lib/use-connector-permissions";
import { effectiveRule, expiresAtFromLifetime, maskedToken, permissionLifetimeLabel, ruleLabel } from "../../lib/permissions";
import { getConnectorModel } from "../../connectors/templates/registry";
import { Badge, CountBadge } from "../ui/badge";
import { Button } from "../ui/button";
import { Select } from "../ui/form";
import { Notice } from "../ui/notice";
import { ConnectorRuleButton } from "../connectors/connector-rule-button";

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
  const activeTokens = tokens.data.filter((token) => !token.revoked_at);
  const [savingKey, setSavingKey] = useState("");
  const [openTokenID, setOpenTokenID] = useState(null);
  const [profileByToken, setProfileByToken] = useState({});
  const [permissionMode, setPermissionMode] = useState("grouped");
  const compactPanelRef = useRef(null);
  const tokenIDsKey = activeTokens.map((token) => token.id).join(",");
  const load = connectorPermissionState || { state: "idle", data: {}, actionsByTargetRef: {}, error: null };
  const permissionsByToken = load.data || {};
  const targetProfiles = useMemo(() => profilesForConnectorTarget(targets?.data || [], selectedTarget), [targets?.data, selectedTarget?.connector_kind, selectedTarget?.target_id]);
  const targetProfileSignature = targetProfiles.map((profile) => profile.profile_id).join(",");

  useEffect(() => {
    if (!selectedTarget) {
      setProfileByToken({});
      return;
    }
    void loadConnectorPermissions();
  }, [selectedTarget?.connector_kind, selectedTarget?.target_id, targetProfileSignature, tokenIDsKey]);

  useEffect(() => {
    if (!selectedTarget || targetProfiles.length === 0) return;
    setProfileByToken((current) => {
      const next = { ...current };
      let changed = false;
      for (const token of activeTokens) {
        const stored = readStoredConnectorProfileID(selectedTarget, token.id);
        const fallbackID = selectedTarget.profile_id || (targetProfiles.length === 1 ? targetProfiles[0].profile_id : "");
        const currentID = current[token.id] || stored || fallbackID;
        const valid = targetProfiles.some((profile) => Number(profile.profile_id) === Number(currentID));
        const nextID = valid ? Number(currentID) : fallbackID ? Number(fallbackID) : "";
        if (String(next[token.id] || "") !== String(nextID || "")) {
          next[token.id] = nextID;
          changed = true;
        }
      }
      return changed ? next : current;
    });
  }, [selectedTarget?.connector_kind, selectedTarget?.target_id, targetProfileSignature, tokenIDsKey]);

  useEffect(() => {
    if (!openTokenID) return undefined;

    function closeOnOutsidePointer(event) {
      if (!compactPanelRef.current?.contains(event.target)) {
        setOpenTokenID(null);
      }
    }

    function closeOnEscape(event) {
      if (event.key === "Escape") {
        setOpenTokenID(null);
      }
    }

    window.addEventListener("pointerdown", closeOnOutsidePointer);
    window.addEventListener("keydown", closeOnEscape);
    return () => {
      window.removeEventListener("pointerdown", closeOnOutsidePointer);
      window.removeEventListener("keydown", closeOnEscape);
    };
  }, [openTokenID]);

  const selectedCountByToken = useMemo(() => {
    const result = {};
    for (const token of activeTokens) {
      const profileID = selectedConnectorProfileID(token.id, selectedTarget, targetProfiles, profileByToken);
      result[token.id] = currentConnectorTargetProfilePermissions(permissionsByToken[token.id] || [], selectedTarget, profileID).length;
    }
    return result;
  }, [activeTokens, permissionsByToken, selectedTarget, targetProfiles, profileByToken]);

  async function loadConnectorPermissions() {
    if (!selectedTarget) return;
    const profilesToLoad = targetProfiles.length > 0 ? targetProfiles : [selectedTarget];
    await Promise.all([
      ...profilesToLoad.map((profile) => loadConnectorActions?.({ ...selectedTarget, profile_id: profile.profile_id || profile.id })),
      loadAllConnectorPermissions?.(activeTokens),
    ]);
  }

  async function refreshPanel() {
    await onRefresh?.();
    await loadConnectorPermissions();
  }

  function selectProfile(token, profileID) {
    const nextID = Number(profileID);
    if (!Number.isFinite(nextID) || nextID <= 0) return;
    setProfileByToken((current) => ({ ...current, [token.id]: nextID }));
    writeStoredConnectorProfileID(selectedTarget, token.id, nextID);
    void loadConnectorActions?.({ ...selectedTarget, profile_id: nextID });
  }

  async function setConnectorRules(token, profileID, selectedActions, rule, keySuffix) {
    if (!selectedTarget) return;
    const key = `${token.id}:${profileID}:${keySuffix}`;
    setSavingKey(key);
    try {
      const existing = permissionsByToken[token.id] || [];
      const actionNames = new Set(selectedActions.map((action) => action.name));
      const preserved = existing.filter((permission) => !matchesConnectorTargetProfile(permission, selectedTarget, profileID) || !actionNames.has(permission.action_name));
      const expiresAt = connectorTargetProfileLifetime(existing, selectedTarget, profileID)?.expires_at || "";
      const next = rule
        ? [
            ...preserved,
            ...selectedActions.map((action) => ({
              target_id: selectedTarget.target_id,
              profile_id: profileID,
              action_name: action.name,
              execution_rule: rule,
              expires_at: expiresAt,
            })),
          ]
        : preserved;
      await replaceTokenConnectorPermissions?.(token.id, next);
    } catch (error) {
      console.error(error);
    } finally {
      setSavingKey("");
    }
  }

  async function setConnectorRule(token, profileID, action, rule) {
    await setConnectorRules(token, profileID, [action], rule, action.name);
  }

  async function setProfileLifetime(token, profileID, expiresAt) {
    if (!selectedTarget) return;
    const key = `${token.id}:${profileID}:lifetime`;
    setSavingKey(key);
    try {
      const existing = permissionsByToken[token.id] || [];
      const next = existing.map((permission) => {
        if (!matchesConnectorTargetProfile(permission, selectedTarget, profileID)) return permission;
        return { ...permission, expires_at: expiresAt || "" };
      });
      await replaceTokenConnectorPermissions?.(token.id, next);
    } catch (error) {
      console.error(error);
    } finally {
      setSavingKey("");
    }
  }

  function renderTokenActions(token, profile, compactPopover = false) {
    const permissions = permissionsByToken[token.id] || [];
    const actions = load.actionsByTargetRef?.[connectorActionCacheKey(selectedTarget, profile.profile_id)] || [];
    const activePermissions = currentConnectorTargetProfilePermissions(permissions, selectedTarget, profile.profile_id);
    const lifetimeValue = connectorTargetProfileLifetime(permissions, selectedTarget, profile.profile_id);
    const categoryGroups = groupActions(actions);
    const riskGroups = groupActionsByRisk(actions);
    return (
      <div className="grid gap-2">
        {activePermissions.length > 0 ? (
          <ProfileLifetimeControls
            value={lifetimeValue}
            saving={savingKey === `${token.id}:${profile.profile_id}:lifetime`}
            onSetPermanent={() => setProfileLifetime(token, profile.profile_id, "")}
            onSetTemporary={(lifetime) => setProfileLifetime(token, profile.profile_id, expiresAtFromLifetime(lifetime))}
          />
        ) : null}
        {actions.length > 0 ? <PermissionModeTabs value={permissionMode} onChange={setPermissionMode} /> : null}
        {permissionMode === "basic" && actions.length > 0 ? (
          <PermissionRuleGroup
            title="All operations"
            description={`${actions.length} connector action${actions.length === 1 ? "" : "s"}`}
            rule={ruleForActions(permissions, selectedTarget, profile.profile_id, actions)}
            saving={savingKey === `${token.id}:${profile.profile_id}:all`}
            onSetRule={(rule) => setConnectorRules(token, profile.profile_id, actions, rule, "all")}
          />
        ) : null}
        {permissionMode === "grouped" && actions.length > 0 ? (
          <div className="grid gap-2">
            {riskGroups.map((group) => (
              <PermissionRuleGroup
                key={group.key}
                title={group.name}
                description={group.description}
                rule={ruleForActions(permissions, selectedTarget, profile.profile_id, group.actions)}
                saving={savingKey === `${token.id}:${profile.profile_id}:${group.key}`}
                disabled={group.actions.length === 0}
                onSetRule={(rule) => setConnectorRules(token, profile.profile_id, group.actions, rule, group.key)}
              />
            ))}
          </div>
        ) : null}
        {permissionMode === "advanced"
          ? categoryGroups.map((group) => (
              <div key={group.name} className="grid gap-2">
                {categoryGroups.length > 1 ? <p className="text-[11px] font-semibold uppercase tracking-wide text-stone-500">{group.name}</p> : null}
                {group.actions.map((action) => {
                  const permission = permissions.find((item) => matchesConnectorTargetProfileAction(item, selectedTarget, profile.profile_id, action.name));
                  const rule = effectiveRule(permission) || "";
                  const key = `${token.id}:${profile.profile_id}:${action.name}`;
                  return (
                    <ActionPermissionCard
                      key={action.name}
                      action={action}
                      rule={rule}
                      saving={savingKey === key}
                      compactPopover={compactPopover}
                      onSetRule={(nextRule) => setConnectorRule(token, profile.profile_id, action, nextRule)}
                    />
                  );
                })}
              </div>
            ))
          : null}
        {load.state === "ready" && actions.length === 0 ? <Notice>No actions exposed by this connector.</Notice> : null}
      </div>
    );
  }

  if (compact) {
    return (
      <aside ref={compactPanelRef} className="relative grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-visible rounded-lg border border-stone-200 bg-white">
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
                    <ProfileSelect profiles={targetProfiles} value={profile?.profile_id} onChange={(profileID) => selectProfile(token, profileID)} />
                    {profile ? renderTokenActions(token, profile, true) : <Notice>No credential profiles for this connector.</Notice>}
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
          <p className="mt-1 truncate text-xs text-stone-500">
            {selectedTarget ? selectedTarget.target_name : "Select a connector"}
          </p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="ghost" className="h-9 w-9 px-0" title="Collapse tokens" onClick={onToggleCompact}>
            <PanelRightClose className="h-4 w-4" />
          </Button>
          <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Refresh connector permissions" onClick={refreshPanel} disabled={load.state === "loading"}>
            <RefreshCcw className="h-4 w-4" />
          </Button>
        </div>
      </header>

      <div className="min-h-0 overflow-auto p-3">
        {load.state === "loading" ? <Notice>Loading connector permissions...</Notice> : null}
        {load.state === "error" ? <Notice tone="bad">{load.error}</Notice> : null}
        {tokens.state === "error" ? <Notice tone="bad">{tokens.error}</Notice> : null}
        {tokens.state === "ready" && tokens.data.length === 0 ? <Notice>Create a token first.</Notice> : null}
        {tokens.state === "ready" && tokens.data.length > 0 && activeTokens.length === 0 ? <Notice>No active tokens.</Notice> : null}

        <div className="mt-3 grid gap-3">
          {activeTokens.map((token) => {
            const selectedCount = selectedCountByToken[token.id] || 0;
            const profile = selectedConnectorProfile(token.id, selectedTarget, targetProfiles, profileByToken);
            const unreadCount = targetSupportsMessages(selectedTarget)
              ? unreadMessages.filter((message) => Number(message.runtime_profile_id) === Number(profile?.runtime_profile_id || selectedTarget.runtime_profile_id) && Number(message.token_id) === Number(token.id)).length
              : 0;
            return (
              <section className={`grid gap-3 rounded-lg border p-3 transition ${selectedCount > 0 ? "border-emerald-200 bg-emerald-50" : "border-stone-200 bg-white"}`} key={token.id}>
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
                  <Badge tone={selectedCount > 0 ? "good" : "neutral"}>{selectedCount > 0 ? `${selectedCount} grants` : ruleLabel("")}</Badge>
                </div>
                <ProfileSelect profiles={targetProfiles} value={profile?.profile_id} onChange={(profileID) => selectProfile(token, profileID)} />
                {profile ? renderTokenActions(token, profile) : <Notice>No credential profiles for this connector.</Notice>}
              </section>
            );
          })}
        </div>
      </div>
    </aside>
  );
}

function targetSupportsMessages(target) {
  if (!target?.runtime_profile_id) return false;
  const model = getConnectorModel(target.connector_kind);
  return Boolean(model?.usesLiveConsole?.({ target }));
}

function ProfileSelect({ profiles, value, onChange }) {
  if (profiles.length <= 1) {
    const profile = profiles[0];
    return profile ? (
      <div className="flex items-center justify-between gap-2 rounded-md border border-stone-200 bg-white/70 px-2 py-1.5 text-xs">
        <span className="font-semibold text-stone-600">Profile</span>
        <span className="truncate font-medium text-stone-900">{profile.profile_label || "default"}</span>
      </div>
    ) : null;
  }
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

function ProfileLifetimeControls({ value, saving, onSetPermanent, onSetTemporary }) {
  return (
    <div className="dark-panel-subtle grid gap-2 rounded-md border border-stone-200 bg-white/70 p-2 text-xs">
      <div className="flex items-center justify-between gap-2">
        <span className="font-semibold text-stone-700">Lifetime</span>
        <span className="text-stone-500">{permissionLifetimeLabel(value)}</span>
      </div>
      <div className="grid grid-cols-4 gap-1">
        <ConnectorRuleButton active={!value?.expires_at} disabled={saving} onClick={onSetPermanent}>
          Keep
        </ConnectorRuleButton>
        <ConnectorRuleButton active={false} disabled={saving} onClick={() => onSetTemporary("1h")}>
          1h
        </ConnectorRuleButton>
        <ConnectorRuleButton active={false} disabled={saving} onClick={() => onSetTemporary("4h")}>
          4h
        </ConnectorRuleButton>
        <ConnectorRuleButton active={false} disabled={saving} onClick={() => onSetTemporary("1d")}>
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
    <div className="dark-panel-subtle grid gap-2 rounded-md border border-stone-200 bg-white/70 p-2">
      <div className="flex min-w-0 items-center justify-between gap-2">
        <div className="min-w-0">
          <p className="truncate text-xs font-semibold text-stone-900">{title}</p>
          <p className="truncate text-xs text-stone-500">{description}</p>
        </div>
        {rule === "mixed" ? <Badge tone="warn">mixed</Badge> : null}
      </div>
      <div className="grid grid-cols-3 gap-1">
        <ConnectorRuleButton active={!rule && !disabled} disabled={saving || disabled} onClick={() => onSetRule("")}>
          Disabled
        </ConnectorRuleButton>
        <ConnectorRuleButton active={rule === "approval_required"} disabled={saving || disabled} onClick={() => onSetRule("approval_required")}>
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
    <div className={`grid gap-2 rounded-md border border-stone-200 bg-white/70 p-2 ${compactPopover ? "" : "dark-panel-subtle"}`}>
      <div className="flex min-w-0 items-center justify-between gap-2">
        <div className="min-w-0">
          <p className="truncate font-mono text-xs font-semibold text-stone-900">{action.name}</p>
          <p className="line-clamp-2 text-xs text-stone-500">{action.description}</p>
        </div>
        <Badge tone={action.risk === "read" ? "good" : "warn"}>{action.risk}</Badge>
      </div>
      <div className="grid grid-cols-3 gap-1">
        <ConnectorRuleButton active={!rule} disabled={saving} onClick={() => onSetRule("")}>
          Disabled
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

function ruleForActions(permissions, target, profileID, actions) {
  if (!target || actions.length === 0) return "";
  const rules = actions.map((action) => {
    const permission = permissions.find((item) => matchesConnectorTargetProfileAction(item, target, profileID, action.name));
    return effectiveRule(permission) || "";
  });
  const unique = new Set(rules);
  if (unique.size <= 1) return rules[0] || "";
  return "mixed";
}

function groupActions(actions) {
  const order = [];
  const groups = new Map();
  for (const action of actions) {
    const name = action.category || "actions";
    if (!groups.has(name)) {
      groups.set(name, []);
      order.push(name);
    }
    groups.get(name).push(action);
  }
  return order.map((name) => ({ name, actions: groups.get(name) || [] }));
}

function groupActionsByRisk(actions) {
  const readActions = actions.filter((action) => action.risk === "read");
  const writeActions = actions.filter((action) => action.risk !== "read");
  return [
    {
      key: "read",
      name: "Read operations",
      description: readActions.length > 0 ? `${readActions.length} read-only action${readActions.length === 1 ? "" : "s"}` : "No read actions exposed.",
      actions: readActions,
    },
    {
      key: "write",
      name: "Write operations",
      description: writeActions.length > 0 ? `${writeActions.length} write-capable action${writeActions.length === 1 ? "" : "s"}` : "No write actions exposed.",
      actions: writeActions,
    },
  ];
}
