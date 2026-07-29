import { useEffect, useMemo, useState } from "react";
import { apiGet, apiPut } from "../../lib/api";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Badge } from "../ui/badge";
import { Notice } from "../ui/notice";
import { ConnectorRuleButton } from "../connectors/connector-rule-button";
import { connectorActionRiskLabel, connectorActionRiskTone } from "../../lib/connector-action-risks";

const emptyLoad = {
  state: "idle",
  catalog: [],
  targets: [],
  actionsByProfile: {},
  permissions: [],
  projectScopes: [],
  capabilityDefinitions: [],
  projectCapabilities: [],
  error: null,
};

export function ConnectorPermissionDialog({ token, onClose, onSaved }) {
  const [load, setLoad] = useState(emptyLoad);
  const [draft, setDraft] = useState({});
  const [save, setSave] = useState({ state: "idle", error: null });
  const [scopeSave, setScopeSave] = useState({ state: "idle", error: null });
  const [capabilitySave, setCapabilitySave] = useState({ state: "idle", error: null });
  const [selectedProfileKey, setSelectedProfileKey] = useState("");
  const [projectDraft, setProjectDraft] = useState({});
  const [capabilityDraft, setCapabilityDraft] = useState({});

  useEffect(() => {
    if (!token) {
      setLoad(emptyLoad);
      setDraft({});
      setSave({ state: "idle", error: null });
      setScopeSave({ state: "idle", error: null });
      setCapabilitySave({ state: "idle", error: null });
      setSelectedProfileKey("");
      setProjectDraft({});
      setCapabilityDraft({});
      return;
    }
    void loadConnectorPermissions(token.id);
  }, [token?.id]);

  const profileGroups = useMemo(() => {
    const groups = [];
    for (const target of load.targets) {
      for (const profile of target.profiles || []) {
        const key = profileActionKey(target.id, profile.id);
        const actions = load.actionsByProfile[key] || [];
        groups.push({ target, profile, actions, key });
      }
    }
    return groups;
  }, [load.targets, load.actionsByProfile]);

  const rows = useMemo(
    () => profileGroups.flatMap((group) => group.actions.map((action) => ({ ...group, action, key: permissionKey(group.target.id, group.profile.id, action.name) }))),
    [profileGroups]
  );

  useEffect(() => {
    if (!selectedProfileKey) return;
    if (!profileGroups.some((group) => group.key === selectedProfileKey)) {
      setSelectedProfileKey("");
    }
  }, [profileGroups, selectedProfileKey]);

  async function loadConnectorPermissions(tokenID) {
    setLoad((current) => ({ ...current, state: "loading", error: null }));
    try {
      const [catalog, targetList, permissions, projectScopes, projectCapabilities] = await Promise.all([
        apiGet("/api/connectors"),
        apiGet("/api/connector-targets/inventory"),
        apiGet(`/api/tokens/${tokenID}/connector-permissions`),
        apiGet(`/api/tokens/${tokenID}/project-scopes`),
        apiGet(`/api/tokens/${tokenID}/project-capabilities`),
      ]);
      const targets = targetList.items || [];
      const actionEntries = targets.flatMap((target) =>
        (target.profiles || []).map((profile) => [profileActionKey(target.id, profile.id), profile.actions || []])
      );
      const actionsByProfile = Object.fromEntries(actionEntries);
      const permissionItems = permissions.items || [];
      const scopeItems = projectScopes.items || [];
      const capabilityItems = projectCapabilities.items || [];
      setLoad({
        state: "ready",
        catalog: catalog.items || [],
        targets,
        actionsByProfile,
        permissions: permissionItems,
        projectScopes: scopeItems,
        capabilityDefinitions: projectCapabilities.definitions || [],
        projectCapabilities: capabilityItems,
        error: null,
      });
      setProjectDraft(Object.fromEntries(scopeItems.map((scope) => [scope.project_id, Boolean(scope.enabled)])));
      setCapabilityDraft(
        Object.fromEntries(
          capabilityItems.map((capability) => [
            projectCapabilityKey(capability.project_id, capability.capability_name),
            capability.execution_rule,
          ])
        )
      );
      setDraft(
        Object.fromEntries(
          permissionItems
            .map((permission) => [
              permissionKey(permission.target_id, permission.profile_id, permission.action_name),
              {
                execution_rule: permission.execution_rule,
                expires_at: permission.expires_at || "",
              },
            ])
        )
      );
    } catch (error) {
      setLoad({ ...emptyLoad, state: "error", error: error.message });
      setDraft({});
      setProjectDraft({});
      setCapabilityDraft({});
    }
  }

  function setProjectCapabilityRule(projectID, capabilityName, rule) {
    setCapabilityDraft((current) => ({
      ...current,
      [projectCapabilityKey(projectID, capabilityName)]: rule,
    }));
  }

  async function saveProjectCapabilities() {
    if (!token) return;
    setCapabilitySave({ state: "saving", error: null });
    try {
      const capabilities = load.projectScopes.flatMap((project) =>
        load.capabilityDefinitions
          .map((definition) => {
            const executionRule = capabilityDraft[projectCapabilityKey(project.project_id, definition.name)] || "";
            if (!executionRule) return null;
            return {
              project_id: project.project_id,
              capability_name: definition.name,
              execution_rule: executionRule,
            };
          })
          .filter(Boolean)
      );
      const result = await apiPut(`/api/tokens/${token.id}/project-capabilities`, { capabilities });
      const items = result.items || [];
      setLoad((current) => ({
        ...current,
        capabilityDefinitions: result.definitions || current.capabilityDefinitions,
        projectCapabilities: items,
      }));
      setCapabilityDraft(
        Object.fromEntries(
          items.map((capability) => [
            projectCapabilityKey(capability.project_id, capability.capability_name),
            capability.execution_rule,
          ])
        )
      );
      setCapabilitySave({ state: "ready", error: null });
      await onSaved?.();
    } catch (error) {
      setCapabilitySave({ state: "error", error: error.message });
    }
  }

  function setRule(key, rule) {
    setDraft((current) => ({
      ...current,
      [key]: rule ? { execution_rule: rule, expires_at: rule === "blocked" ? "" : current[key]?.expires_at || "" } : { execution_rule: "", expires_at: "" },
    }));
  }

  async function toggleProjectScope(projectID, enabled) {
    if (!token) return;
    const nextDraft = { ...projectDraft, [projectID]: enabled };
    setProjectDraft(nextDraft);
    setScopeSave({ state: "saving", error: null });
    try {
      const enabledProjectIDs = load.projectScopes.filter((scope) => nextDraft[scope.project_id]).map((scope) => scope.project_id);
      const result = await apiPut(`/api/tokens/${token.id}/project-scopes`, { enabled_project_ids: enabledProjectIDs });
      const items = result.items || [];
      setLoad((current) => ({ ...current, projectScopes: items }));
      setProjectDraft(Object.fromEntries(items.map((scope) => [scope.project_id, Boolean(scope.enabled)])));
      await onSaved?.();
      setScopeSave({ state: "ready", error: null });
    } catch (error) {
      setProjectDraft(projectDraft);
      setScopeSave({ state: "error", error: error.message });
    }
  }

  async function savePermissions(event) {
    event.preventDefault();
    if (!token) return;
    setSave({ state: "saving", error: null });
    try {
      const knownKeys = new Set(rows.map((row) => row.key));
      const preserved = load.permissions
        .filter((permission) => !knownKeys.has(permissionKey(permission.target_id, permission.profile_id, permission.action_name)))
        .map((permission) => ({
          target_id: permission.target_id,
          profile_id: permission.profile_id,
          action_name: permission.action_name,
          execution_rule: permission.execution_rule,
          expires_at: permission.execution_rule === "blocked" ? undefined : permission.expires_at || undefined,
        }));
      const connectorPermissions = rows
        .map((row) => {
          const permission = draft[row.key];
          if (!permission?.execution_rule) return null;
          return {
            target_id: row.target.id,
            profile_id: row.profile.id,
            action_name: row.action.name,
            execution_rule: permission.execution_rule,
            expires_at: permission.execution_rule === "blocked" ? undefined : permission.expires_at || undefined,
          };
        })
        .filter(Boolean);
      const result = await apiPut(`/api/tokens/${token.id}/connector-permissions`, { permissions: [...preserved, ...connectorPermissions] });
      setLoad((current) => ({ ...current, permissions: result.items || [] }));
      await onSaved?.();
      setSave({ state: "ready", error: null });
    } catch (error) {
      setSave({ state: "error", error: error.message });
    }
  }

  const selectedCount = Object.values(draft).filter((permission) => Boolean(permission?.execution_rule)).length;
  const selectedProfile = profileGroups.find((group) => group.key === selectedProfileKey) || null;

  return (
    <Dialog
      open={Boolean(token)}
      title={token ? `${token.name} connector permissions` : "Connector permissions"}
      description="Grant this token access to connector capabilities."
      onClose={onClose}
      size="wide"
      className="!max-w-[1120px]"
      bodyClassName="max-h-[calc(100vh-180px)] overflow-y-auto"
    >
      <form className="grid gap-4" onSubmit={savePermissions}>
        <Notice>
          Security note: each connector grant binds one target, one credential profile, and one action. Prefer Prompt until you trust the workflow.
        </Notice>
        {load.state === "loading" ? <Notice>Loading connector targets...</Notice> : null}
        {load.state === "error" ? <Notice tone="bad">{load.error}</Notice> : null}
        {save.state === "error" ? <Notice tone="bad">{save.error}</Notice> : null}
        {scopeSave.state === "error" ? <Notice tone="bad">{scopeSave.error}</Notice> : null}
        {capabilitySave.state === "error" ? <Notice tone="bad">{capabilitySave.error}</Notice> : null}
        {save.state === "ready" ? <Notice tone="good">Connector permissions saved.</Notice> : null}
        {capabilitySave.state === "ready" ? <Notice tone="good">Project Vault capabilities saved.</Notice> : null}
        {load.state === "ready" && rows.length === 0 ? <Notice>Create a connector target before granting action permissions.</Notice> : null}

        {load.state === "ready" ? (
          <div className="grid gap-2 rounded-lg border border-stone-200 bg-stone-50 p-3">
            <div>
              <p className="text-xs font-semibold uppercase text-stone-500">Project visibility</p>
              <p className="mt-0.5 text-xs text-stone-500">Disabled projects stay configured, but disappear from this token's MCP target list. Changes apply immediately.</p>
            </div>
            <div className="flex flex-wrap gap-2">
              {load.projectScopes.map((scope) => (
                <label key={scope.project_id} className={`inline-flex cursor-pointer items-center gap-2 rounded-md border px-3 py-2 text-xs font-semibold ${projectDraft[scope.project_id] ? "border-emerald-700 bg-emerald-950 text-white" : "border-stone-300 bg-white text-stone-600"}`}>
                  <input
                    type="checkbox"
                    className="h-3.5 w-3.5 accent-emerald-700"
                    checked={Boolean(projectDraft[scope.project_id])}
                    disabled={scopeSave.state === "saving"}
                    onChange={(event) => void toggleProjectScope(scope.project_id, event.target.checked)}
                  />
                  {scope.project_name}
                </label>
              ))}
            </div>
          </div>
        ) : null}

        {load.state === "ready" && load.capabilityDefinitions.length > 0 ? (
          <section className="grid gap-3 rounded-lg border border-stone-200 bg-white p-3">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <p className="text-xs font-semibold uppercase text-stone-500">Project Vault capabilities</p>
                <p className="mt-0.5 text-xs text-stone-500">
                  Vault metadata and session access are explicit project grants. Enabling project visibility does not grant them.
                </p>
              </div>
              <Button
                type="button"
                variant="outline"
                disabled={capabilitySave.state === "saving"}
                onClick={() => void saveProjectCapabilities()}
              >
                {capabilitySave.state === "saving" ? "Saving..." : "Save Vault capabilities"}
              </Button>
            </div>
            <div className="grid max-h-[280px] gap-2 overflow-y-auto pr-1">
              {load.projectScopes.map((project) => (
                <div key={project.project_id} className={`grid gap-2 rounded-md border border-stone-200 p-3 ${projectDraft[project.project_id] ? "" : "opacity-60"}`}>
                  <div className="flex min-w-0 items-center justify-between gap-2">
                    <span className="truncate text-sm font-semibold text-stone-950">{project.project_name}</span>
                    <Badge tone={projectDraft[project.project_id] ? "good" : "warn"}>
                      {projectDraft[project.project_id] ? "visible" : "hidden"}
                    </Badge>
                  </div>
                  <div className="grid gap-2 xl:grid-cols-3">
                    {load.capabilityDefinitions.map((definition) => {
                      const key = projectCapabilityKey(project.project_id, definition.name);
                      const rule = capabilityDraft[key] || "";
                      return (
                        <div key={definition.name} className="grid gap-2 rounded-md bg-stone-50 p-2">
                          <div className="min-w-0">
                            <p className="truncate font-mono text-xs font-semibold text-stone-950">{definition.label}</p>
                            <p className="line-clamp-2 text-xs text-stone-500">{definition.description}</p>
                          </div>
                          <div className={`grid gap-1 ${definition.allowed_rules.length === 2 ? "grid-cols-3" : "grid-cols-4"}`}>
                            <ConnectorRuleButton active={!rule} onClick={() => setProjectCapabilityRule(project.project_id, definition.name, "")}>
                              Disabled
                            </ConnectorRuleButton>
                            {definition.allowed_rules.includes("blocked") ? (
                              <ConnectorRuleButton active={rule === "blocked"} onClick={() => setProjectCapabilityRule(project.project_id, definition.name, "blocked")}>
                                Blocked
                              </ConnectorRuleButton>
                            ) : null}
                            {definition.allowed_rules.includes("approval_required") ? (
                              <ConnectorRuleButton active={rule === "approval_required"} onClick={() => setProjectCapabilityRule(project.project_id, definition.name, "approval_required")}>
                                Prompt
                              </ConnectorRuleButton>
                            ) : null}
                            {definition.allowed_rules.includes("always_run") ? (
                              <ConnectorRuleButton active={rule === "always_run"} onClick={() => setProjectCapabilityRule(project.project_id, definition.name, "always_run")}>
                                Always
                              </ConnectorRuleButton>
                            ) : null}
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </div>
              ))}
            </div>
          </section>
        ) : null}

        {load.state === "ready" && rows.length > 0 ? (
          <div
            className="grid overflow-hidden rounded-lg border border-stone-200 bg-white lg:grid-cols-[320px_minmax(0,1fr)]"
            style={{ height: "clamp(360px, calc(100vh - 320px), 560px)" }}
          >
            <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] border-b border-stone-200 lg:border-b-0 lg:border-r">
              <div className="border-b border-stone-200 bg-stone-50 px-3 py-2">
                <p className="text-xs font-semibold uppercase text-stone-500">Connectors</p>
                <p className="mt-0.5 text-xs text-stone-500">Select a target profile to edit its rules.</p>
              </div>
              <div className="min-h-0 divide-y divide-stone-200 overflow-y-auto">
                {profileGroups.map((group) => {
                  const activeCount = activeRuleCount(group, draft);
                  const selected = group.key === selectedProfileKey;
                  const projectEnabled = Boolean(projectDraft[group.target.project_id]);
                  return (
                    <button
                      key={group.key}
                      type="button"
                      className={`grid w-full gap-1 px-3 py-3 text-left transition ${
                        selected ? "bg-emerald-950 text-white" : "bg-white text-stone-950 hover:bg-stone-50"
                      } ${projectEnabled ? "" : "opacity-60"}`}
                      onClick={() => setSelectedProfileKey((current) => (current === group.key ? "" : group.key))}
                    >
                      <div className="flex min-w-0 items-center justify-between gap-2">
                        <span className="truncate text-sm font-semibold">{group.target.name}</span>
                        <Badge tone={activeCount > 0 ? "good" : "neutral"}>{activeCount}/{group.actions.length}</Badge>
                      </div>
                      <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-xs">
                        <Badge tone="neutral">{connectorLabel(load.catalog, group.target.connector_kind)}</Badge>
                        <Badge tone={projectEnabled ? "good" : "warn"}>{group.target.project_name || "Ungrouped"}</Badge>
                        <span className={selected ? "truncate text-emerald-50" : "truncate text-stone-500"}>{group.profile.label}</span>
                      </div>
                    </button>
                  );
                })}
              </div>
            </div>
            <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)]">
              <div className="border-b border-stone-200 bg-stone-50 px-3 py-2">
                <p className="text-xs font-semibold uppercase text-stone-500">Actions</p>
                <p className="mt-0.5 truncate text-xs text-stone-500">
                  {selectedProfile
                    ? `${selectedProfile.target.name} / ${selectedProfile.profile.label}`
                    : "Choose a connector profile from the left."}
                </p>
              </div>
              {selectedProfile ? (
                <div className="min-h-0 divide-y divide-stone-200 overflow-y-auto">
                  {selectedProfile.actions.map((action) => {
                    const key = permissionKey(selectedProfile.target.id, selectedProfile.profile.id, action.name);
                    const rule = draft[key]?.execution_rule || "";
                    return (
                      <div key={key} className="grid gap-3 px-3 py-3 md:grid-cols-[minmax(0,1fr)_230px]">
                        <div className="grid min-w-0 gap-1">
                          <div className="flex min-w-0 flex-wrap items-center gap-2">
                            <span className="truncate font-mono text-xs font-semibold text-stone-950">{action.name}</span>
                            <Badge tone={connectorActionRiskTone(action.risk)}>{connectorActionRiskLabel(action.risk)}</Badge>
                          </div>
                          <span className="line-clamp-2 text-xs text-stone-500">{action.description}</span>
                        </div>
                        <div className="grid grid-cols-4 gap-1 self-start">
                          <ConnectorRuleButton active={!rule} onClick={() => setRule(key, "")}>
                            Disabled
                          </ConnectorRuleButton>
                          <ConnectorRuleButton active={rule === "blocked"} onClick={() => setRule(key, "blocked")}>
                            Blocked
                          </ConnectorRuleButton>
                          <ConnectorRuleButton active={rule === "approval_required"} onClick={() => setRule(key, "approval_required")}>
                            Prompt
                          </ConnectorRuleButton>
                          <ConnectorRuleButton active={rule === "always_run"} onClick={() => setRule(key, "always_run")}>
                            Always
                          </ConnectorRuleButton>
                        </div>
                      </div>
                    );
                  })}
                </div>
              ) : (
                <div className="grid min-h-[260px] place-items-center p-6 text-center text-sm text-stone-500">
                  Select a connector profile to review and grant its available actions.
                </div>
              )}
            </div>
          </div>
        ) : null}

        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-sm text-stone-500">{selectedCount} connector action grant{selectedCount === 1 ? "" : "s"} selected.</p>
          <div className="flex gap-2">
            <Button type="button" variant="outline" onClick={onClose}>
              Close
            </Button>
            <Button type="submit" disabled={!token || load.state !== "ready" || save.state === "saving"}>
              {save.state === "saving" ? "Saving..." : "Save connector permissions"}
            </Button>
          </div>
        </div>
      </form>
    </Dialog>
  );
}

function permissionKey(targetID, profileID, actionName) {
  return `${targetID}:${profileID}:${actionName}`;
}

function profileActionKey(targetID, profileID) {
  return `${targetID}:${profileID}`;
}

function projectCapabilityKey(projectID, capabilityName) {
  return `${projectID}:${capabilityName}`;
}

function connectorLabel(catalog, kind) {
  return catalog.find((item) => item.kind === kind)?.label || kind;
}

function activeRuleCount(group, draft) {
  return group.actions.filter((action) => Boolean(draft[permissionKey(group.target.id, group.profile.id, action.name)]?.execution_rule)).length;
}
