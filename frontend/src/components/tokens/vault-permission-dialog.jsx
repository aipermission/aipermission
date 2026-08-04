import { useEffect, useMemo, useState } from "react";
import { apiGet, apiPut } from "../../lib/api";
import { updateTokenProjectVisibility } from "../../lib/project-scopes";
import { ConnectorRuleButton } from "../connectors/connector-rule-button";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Notice } from "../ui/notice";
import { expiresAtFromLifetime, permissionLifetimeLabel } from "../../lib/permissions";
import { vaultCapabilitiesFromDraft, vaultCapabilityDraftFromItems, vaultCapabilityKey } from "../../lib/vault-capabilities";

const emptyLoad = {
  state: "idle",
  projects: [],
  definitions: [],
  capabilities: [],
  error: null,
};

export function VaultPermissionDialog({ token, onClose, onSaved }) {
  const [load, setLoad] = useState(emptyLoad);
  const [scopeDraft, setScopeDraft] = useState({});
  const [capabilityDraft, setCapabilityDraft] = useState({});
  const [selectedProjectID, setSelectedProjectID] = useState(0);
  const [scopeSave, setScopeSave] = useState({ state: "idle", error: null });
  const [save, setSave] = useState({ state: "idle", error: null });

  useEffect(() => {
    if (!token) {
      setLoad(emptyLoad);
      setScopeDraft({});
      setCapabilityDraft({});
      setSelectedProjectID(0);
      setScopeSave({ state: "idle", error: null });
      setSave({ state: "idle", error: null });
      return;
    }
    void loadVaultPermissions(token.id);
  }, [token?.id]);

  const selectedProject = useMemo(
    () => load.projects.find((project) => project.project_id === selectedProjectID) || null,
    [load.projects, selectedProjectID],
  );

  useEffect(() => {
    if (load.state !== "ready") return;
    if (selectedProjectID && load.projects.some((project) => project.project_id === selectedProjectID)) return;
    setSelectedProjectID(load.projects[0]?.project_id || 0);
  }, [load.state, load.projects, selectedProjectID]);

  async function loadVaultPermissions(tokenID) {
    setLoad((current) => ({ ...current, state: "loading", error: null }));
    try {
      const [projectScopes, projectCapabilities] = await Promise.all([
        apiGet(`/api/tokens/${tokenID}/project-scopes`),
        apiGet(`/api/tokens/${tokenID}/project-capabilities`),
      ]);
      const projects = projectScopes.items || [];
      const definitions = projectCapabilities.definitions || [];
      const capabilities = projectCapabilities.items || [];
      setLoad({ state: "ready", projects, definitions, capabilities, error: null });
      setScopeDraft(Object.fromEntries(projects.map((project) => [project.project_id, Boolean(project.enabled)])));
      setCapabilityDraft(vaultCapabilityDraftFromItems(capabilities, definitions));
    } catch (error) {
      setLoad({ ...emptyLoad, state: "error", error: error.message });
      setScopeDraft({});
      setCapabilityDraft({});
    }
  }

  async function toggleProjectScope(projectID, enabled) {
    if (!token) return;
    const previousDraft = scopeDraft;
    const nextDraft = { ...scopeDraft, [projectID]: enabled };
    setScopeDraft(nextDraft);
    setScopeSave({ state: "saving", error: null });
    try {
      const projectsWithDraft = load.projects.map((project) => ({
        ...project,
        enabled: Boolean(nextDraft[project.project_id]),
      }));
      const result = await updateTokenProjectVisibility(token.id, projectsWithDraft, projectID, enabled);
      const projects = result.items || [];
      setLoad((current) => ({ ...current, projects }));
      setScopeDraft(Object.fromEntries(projects.map((project) => [project.project_id, Boolean(project.enabled)])));
      setScopeSave({ state: "ready", error: null });
      await onSaved?.();
    } catch (error) {
      setScopeDraft(previousDraft);
      setScopeSave({ state: "error", error: error.message });
    }
  }

  function setCapabilityRule(projectID, capabilityName, executionRule) {
    const key = vaultCapabilityKey(projectID, capabilityName);
    setCapabilityDraft((current) => ({
      ...current,
      [key]: executionRule
        ? { execution_rule: executionRule, expires_at: current[key]?.expires_at || "" }
        : { execution_rule: "", expires_at: "" },
    }));
  }

  function setCapabilityLifetime(projectID, capabilityName, lifetime) {
    const key = vaultCapabilityKey(projectID, capabilityName);
    setCapabilityDraft((current) => ({
      ...current,
      [key]: {
        execution_rule: current[key]?.execution_rule || "",
        expires_at: lifetime === "permanent" ? "" : expiresAtFromLifetime(lifetime),
      },
    }));
  }

  async function saveCapabilities(event) {
    event.preventDefault();
    if (!token) return;
    setSave({ state: "saving", error: null });
    try {
      const capabilities = vaultCapabilitiesFromDraft(load.projects, load.definitions, capabilityDraft);
      const result = await apiPut(`/api/tokens/${token.id}/project-capabilities`, { capabilities });
      const definitions = result.definitions || load.definitions;
      const items = result.items || [];
      setLoad((current) => ({
        ...current,
        definitions,
        capabilities: items,
      }));
      setCapabilityDraft(vaultCapabilityDraftFromItems(items, definitions));
      setSave({ state: "ready", error: null });
      await onSaved?.();
    } catch (error) {
      setSave({ state: "error", error: error.message });
    }
  }

  const selectedCount = Object.values(capabilityDraft).filter((permission) => Boolean(permission?.execution_rule)).length;

  return (
    <Dialog
      open={Boolean(token)}
      title={token ? `${token.name} Vault permissions` : "Vault permissions"}
      description="Control project visibility and Vault capabilities for this token."
      onClose={onClose}
      size="wide"
      className="!max-w-[1120px]"
      bodyClassName="max-h-[calc(100vh-180px)] overflow-hidden"
    >
      <form className="grid gap-4" onSubmit={saveCapabilities}>
        <Notice tone="warn">
          Project visibility does not grant Vault access. Prompt asks before each action. Always permits autonomous secret generation or
          delivery through the same validation, lease, and drift checks, without opening an approval dialog.
        </Notice>
        {load.state === "loading" ? <Notice>Loading project Vault permissions...</Notice> : null}
        {load.state === "error" ? <Notice tone="bad">{load.error}</Notice> : null}
        {scopeSave.state === "error" ? <Notice tone="bad">{scopeSave.error}</Notice> : null}
        {save.state === "error" ? <Notice tone="bad">{save.error}</Notice> : null}
        {save.state === "ready" ? <Notice tone="good">Project Vault capabilities saved.</Notice> : null}
        {load.state === "ready" && load.projects.length === 0 ? (
          <Notice>Create a project before granting Vault capabilities.</Notice>
        ) : null}

        {load.state === "ready" && load.projects.length > 0 ? (
          <div
            className="grid overflow-hidden rounded-lg border border-stone-200 bg-white lg:grid-cols-[320px_minmax(0,1fr)]"
            style={{ height: "clamp(360px, calc(100vh - 320px), 560px)" }}
          >
            <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] border-b border-stone-200 lg:border-b-0 lg:border-r">
              <div className="border-b border-stone-200 bg-stone-50 px-3 py-2">
                <p className="text-xs font-semibold uppercase text-stone-500">Projects</p>
                <p className="mt-0.5 text-xs text-stone-500">Select a project and control whether this token can discover it.</p>
              </div>
              <div className="min-h-0 divide-y divide-stone-200 overflow-y-auto">
                {load.projects.map((project) => {
                  const selected = project.project_id === selectedProjectID;
                  const visible = Boolean(scopeDraft[project.project_id]);
                  const activeCount = load.definitions.filter((definition) =>
                    Boolean(capabilityDraft[vaultCapabilityKey(project.project_id, definition.name)]?.execution_rule),
                  ).length;
                  return (
                    <div
                      key={project.project_id}
                      className={`grid grid-cols-[minmax(0,1fr)_auto] items-center transition ${
                        selected ? "bg-emerald-950 text-white" : "bg-white text-stone-950 hover:bg-stone-50"
                      }`}
                    >
                      <button
                        type="button"
                        className="grid min-w-0 gap-1 px-3 py-3 text-left"
                        onClick={() => setSelectedProjectID((current) => (current === project.project_id ? 0 : project.project_id))}
                      >
                        <span className="truncate text-sm font-semibold">{project.project_name}</span>
                        <span className={`truncate text-xs ${selected ? "text-emerald-50" : "text-stone-500"}`}>
                          {activeCount}/{load.definitions.length} Vault capabilities
                        </span>
                      </button>
                      <label className="mr-3 inline-flex cursor-pointer items-center gap-2 text-xs font-semibold">
                        <input
                          type="checkbox"
                          className="h-3.5 w-3.5 accent-emerald-700"
                          aria-label={`${project.project_name} project visibility`}
                          checked={visible}
                          disabled={scopeSave.state === "saving"}
                          onChange={(event) => void toggleProjectScope(project.project_id, event.target.checked)}
                        />
                        <span>{visible ? "Visible" : "Hidden"}</span>
                      </label>
                    </div>
                  );
                })}
              </div>
            </div>

            <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)]">
              <div className="border-b border-stone-200 bg-stone-50 px-3 py-2">
                <div className="flex min-w-0 items-center justify-between gap-2">
                  <div className="min-w-0">
                    <p className="text-xs font-semibold uppercase text-stone-500">Vault capabilities</p>
                    <p className="mt-0.5 truncate text-xs text-stone-500">
                      {selectedProject ? selectedProject.project_name : "Choose a project from the left."}
                    </p>
                  </div>
                  {selectedProject ? (
                    <Badge tone={scopeDraft[selectedProject.project_id] ? "good" : "warn"}>
                      {scopeDraft[selectedProject.project_id] ? "visible" : "hidden"}
                    </Badge>
                  ) : null}
                </div>
              </div>
              {selectedProject ? (
                <div className="min-h-0 divide-y divide-stone-200 overflow-y-auto">
                  {load.definitions.map((definition) => {
                    const key = vaultCapabilityKey(selectedProject.project_id, definition.name);
                    const permission = capabilityDraft[key] || { execution_rule: "", expires_at: "" };
                    const rule = permission.execution_rule;
                    return (
                      <div key={definition.name} className="grid gap-3 px-3 py-4 md:grid-cols-[minmax(0,1fr)_280px]">
                        <div className="grid min-w-0 gap-1">
                          <span className="truncate font-mono text-xs font-semibold text-stone-950">{definition.label}</span>
                          <span className="text-xs text-stone-500">{definition.description}</span>
                        </div>
                        <div
                          className="grid gap-1 self-start"
                          style={{ gridTemplateColumns: `repeat(${definition.allowed_rules.length + 1}, minmax(0, 1fr))` }}
                        >
                          <ConnectorRuleButton
                            active={!rule}
                            onClick={() => setCapabilityRule(selectedProject.project_id, definition.name, "")}
                          >
                            Disabled
                          </ConnectorRuleButton>
                          {definition.allowed_rules.includes("approval_required") ? (
                            <ConnectorRuleButton
                              active={rule === "approval_required"}
                              onClick={() => setCapabilityRule(selectedProject.project_id, definition.name, "approval_required")}
                            >
                              Prompt
                            </ConnectorRuleButton>
                          ) : null}
                          {definition.allowed_rules.includes("always_run") ? (
                            <ConnectorRuleButton
                              active={rule === "always_run"}
                              onClick={() => setCapabilityRule(selectedProject.project_id, definition.name, "always_run")}
                            >
                              Always
                            </ConnectorRuleButton>
                          ) : null}
                        </div>
                        <div className="dark-panel-subtle grid gap-2 rounded-md border border-stone-200 bg-white/70 p-2 text-xs md:col-start-2">
                          <div className="flex items-center justify-between gap-2">
                            <span className="font-semibold text-stone-700">Lifetime</span>
                            <span className="text-stone-500">{rule ? permissionLifetimeLabel(permission) : "Disabled"}</span>
                          </div>
                          <div className="grid grid-cols-4 gap-1">
                            <ConnectorRuleButton
                              active={Boolean(rule) && !permission.expires_at}
                              disabled={!rule}
                              onClick={() => setCapabilityLifetime(selectedProject.project_id, definition.name, "permanent")}
                            >
                              Keep
                            </ConnectorRuleButton>
                            {["1h", "4h", "1d"].map((lifetime) => (
                              <ConnectorRuleButton
                                key={lifetime}
                                active={false}
                                disabled={!rule}
                                onClick={() => setCapabilityLifetime(selectedProject.project_id, definition.name, lifetime)}
                              >
                                {lifetime}
                              </ConnectorRuleButton>
                            ))}
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              ) : (
                <div className="grid min-h-[260px] place-items-center p-6 text-center text-sm text-stone-500">
                  Select a project to review and grant its Vault capabilities.
                </div>
              )}
            </div>
          </div>
        ) : null}

        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-sm text-stone-500">
            {selectedCount} Vault capability grant{selectedCount === 1 ? "" : "s"} selected.
          </p>
          <div className="flex gap-2">
            <Button type="button" variant="outline" onClick={onClose}>
              Close
            </Button>
            <Button type="submit" disabled={!token || load.state !== "ready" || save.state === "saving"}>
              {save.state === "saving" ? "Saving..." : "Save Vault capabilities"}
            </Button>
          </div>
        </div>
      </form>
    </Dialog>
  );
}
