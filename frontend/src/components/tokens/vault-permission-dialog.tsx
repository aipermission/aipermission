import { useMemo } from "react";
import type { Dispatch, SetStateAction } from "react";
import type { TokenProjectScope } from "../../lib/gateway-contracts/security-contracts";
import { ConnectorRuleButton } from "../connectors/connector-rule-button";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Notice } from "../ui/notice";
import { permissionLifetimeLabel } from "../../lib/permissions";
import { vaultCapabilityKey } from "../../lib/vault-capabilities";
import type { VaultCapabilityDefinition, VaultCapabilityDraft } from "../../lib/vault-capabilities";
import { useVaultPermissionEditor } from "./use-vault-permission-editor";
import type { VaultPermissionLoad as Load, VaultPermissionSave as Save } from "./use-vault-permission-editor";

type Props = { token: { id: number; name: string } | null; onClose: () => void; onSaved?: () => void | Promise<void> };

export function VaultPermissionDialog({ token, onClose, onSaved }: Props) {
  const {
    load,
    scopeDraft,
    capabilityDraft,
    selectedProjectID,
    setSelectedProjectID,
    scopeSave,
    save,
    toggleProjectScope,
    setCapabilityRule,
    setCapabilityLifetime,
    saveCapabilities,
  } = useVaultPermissionEditor(token?.id, onSaved);
  const selectedProject = useMemo(
    () => load.projects.find((project) => project.project_id === selectedProjectID) || null,
    [load.projects, selectedProjectID],
  );

  const selectedCount = Object.values(capabilityDraft).filter((permission) => Boolean(permission?.execution_rule)).length;

  return (
    <Dialog
      open={Boolean(token)}
      title={token ? `${token.name} Vault permissions` : "Vault permissions"}
      description="Control project visibility and Vault capabilities for this token."
      onClose={onClose}
      closeDisabled={scopeSave.state === "saving" || save.state === "saving"}
      size="wide"
      className="!max-w-[1120px]"
      bodyClassName="max-h-[calc(100dvh-180px)] overflow-y-auto"
    >
      <form className="grid min-w-0 gap-4" onSubmit={saveCapabilities}>
        <VaultDialogNotices load={load} scopeSave={scopeSave} save={save} />

        {load.state === "ready" && load.projects.length > 0 ? (
          <div className="grid h-[clamp(360px,calc(100vh-320px),560px)] overflow-hidden rounded-lg border border-stone-200 bg-white lg:grid-cols-[320px_minmax(0,1fr)]">
            <VaultProjectList
              projects={load.projects}
              definitions={load.definitions}
              selectedProjectID={selectedProjectID}
              setSelectedProjectID={setSelectedProjectID}
              scopeDraft={scopeDraft}
              scopeSaving={scopeSave.state === "saving"}
              capabilityDraft={capabilityDraft}
              onToggleScope={toggleProjectScope}
            />

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
                        <div className={`grid gap-1 self-start ${vaultRuleGridClass(definition.allowed_rules.length)}`}>
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

        <VaultDialogFooter selectedCount={selectedCount} token={token} loadState={load.state} saveState={save.state} onClose={onClose} />
      </form>
    </Dialog>
  );
}

function VaultDialogNotices({ load, scopeSave, save }: { load: Load; scopeSave: Save; save: Save }) {
  return (
    <>
      <Notice tone="warn">
        Project visibility does not grant Vault access. Prompt asks before each action. Always permits autonomous secret generation or
        delivery through the same validation, lease, and drift checks, without opening an approval dialog.
      </Notice>
      {load.state === "loading" ? <Notice>Loading project Vault permissions...</Notice> : null}
      {load.state === "error" ? <Notice tone="bad">{load.error}</Notice> : null}
      {scopeSave.error ? <Notice tone="bad">{scopeSave.error}</Notice> : null}
      {save.error ? <Notice tone="bad">{save.error}</Notice> : null}
      {save.state === "ready" ? <Notice tone="good">Project Vault capabilities saved.</Notice> : null}
      {save.state === "unsaved" ? <Notice tone="warn">Submitted Vault capabilities saved. Newer edits are not saved.</Notice> : null}
      {load.state === "ready" && load.projects.length === 0 ? <Notice>Create a project before granting Vault capabilities.</Notice> : null}
    </>
  );
}

function VaultProjectList({
  projects,
  definitions,
  selectedProjectID,
  setSelectedProjectID,
  scopeDraft,
  scopeSaving,
  capabilityDraft,
  onToggleScope,
}: {
  projects: TokenProjectScope[];
  definitions: VaultCapabilityDefinition[];
  selectedProjectID: number;
  setSelectedProjectID: Dispatch<SetStateAction<number>>;
  scopeDraft: Record<number, boolean>;
  scopeSaving: boolean;
  capabilityDraft: VaultCapabilityDraft;
  onToggleScope: (_projectID: number, _enabled: boolean) => void;
}) {
  return (
    <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] border-b border-stone-200 lg:border-b-0 lg:border-r">
      <div className="border-b border-stone-200 bg-stone-50 px-3 py-2">
        <p className="text-xs font-semibold uppercase text-stone-500">Projects</p>
        <p className="mt-0.5 text-xs text-stone-500">Select a project and control whether this token can discover it.</p>
      </div>
      <div className="min-h-0 divide-y divide-stone-200 overflow-y-auto">
        {projects.map((project) => (
          <VaultProjectRow
            key={project.project_id}
            project={project}
            definitions={definitions}
            selected={project.project_id === selectedProjectID}
            visible={Boolean(scopeDraft[project.project_id])}
            scopeSaving={scopeSaving}
            capabilityDraft={capabilityDraft}
            onSelect={() => setSelectedProjectID((current) => (current === project.project_id ? 0 : project.project_id))}
            onToggleScope={onToggleScope}
          />
        ))}
      </div>
    </div>
  );
}

function VaultProjectRow({
  project,
  definitions,
  selected,
  visible,
  scopeSaving,
  capabilityDraft,
  onSelect,
  onToggleScope,
}: {
  project: TokenProjectScope;
  definitions: VaultCapabilityDefinition[];
  selected: boolean;
  visible: boolean;
  scopeSaving: boolean;
  capabilityDraft: VaultCapabilityDraft;
  onSelect: () => void;
  onToggleScope: (_projectID: number, _enabled: boolean) => void;
}) {
  const activeCount = definitions.filter((definition) =>
    Boolean(capabilityDraft[vaultCapabilityKey(project.project_id, definition.name)]?.execution_rule),
  ).length;
  return (
    <div
      className={`grid grid-cols-[minmax(0,1fr)_auto] items-center transition ${selected ? "bg-emerald-950 text-white" : "bg-white text-stone-950 hover:bg-stone-50"}`}
    >
      <button type="button" className="grid min-w-0 gap-1 px-3 py-3 text-left" onClick={onSelect}>
        <span className="truncate text-sm font-semibold">{project.project_name}</span>
        <span className={`truncate text-xs ${selected ? "text-emerald-50" : "text-stone-500"}`}>
          {activeCount}/{definitions.length} Vault capabilities
        </span>
      </button>
      <label className="mr-3 inline-flex cursor-pointer items-center gap-2 text-xs font-semibold">
        <input
          type="checkbox"
          className="h-3.5 w-3.5 accent-emerald-700"
          aria-label={`${project.project_name} project visibility`}
          checked={visible}
          disabled={scopeSaving}
          onChange={(event) => void onToggleScope(project.project_id, event.target.checked)}
        />
        <span>{visible ? "Visible" : "Hidden"}</span>
      </label>
    </div>
  );
}

function VaultDialogFooter({
  selectedCount,
  token,
  loadState,
  saveState,
  onClose,
}: {
  selectedCount: number;
  token: Props["token"];
  loadState: Load["state"];
  saveState: Save["state"];
  onClose: () => void;
}) {
  return (
    <div className="grid items-center gap-3 sm:flex sm:flex-wrap sm:justify-between">
      <p className="text-sm text-stone-500">
        {selectedCount} Vault capability grant{selectedCount === 1 ? "" : "s"} selected.
      </p>
      <div className="grid w-full gap-2 min-[380px]:grid-cols-[auto_minmax(0,1fr)] sm:w-auto sm:flex">
        <Button type="button" variant="outline" onClick={onClose}>
          Close
        </Button>
        <Button type="submit" disabled={!token || loadState !== "ready" || saveState === "saving"}>
          {saveState === "saving" ? "Saving..." : "Save Vault capabilities"}
        </Button>
      </div>
    </div>
  );
}

function vaultRuleGridClass(ruleCount: number) {
  if (ruleCount >= 2) return "grid-cols-3";
  if (ruleCount === 1) return "grid-cols-2";
  return "grid-cols-1";
}
