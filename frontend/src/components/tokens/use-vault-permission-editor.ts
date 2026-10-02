import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { Dispatch, FormEvent, SetStateAction } from "react";
import { apiGet, apiPut } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import { updateTokenProjectVisibility } from "../../lib/project-scopes";
import { tokenProjectScopeSnapshot } from "../../lib/gateway-contracts/security-contracts";
import type { TokenProjectScope } from "../../lib/gateway-contracts/security-contracts";
import { expiresAtFromLifetime } from "../../lib/permissions";
import {
  vaultCapabilitiesFromDraft,
  vaultCapabilityDraftFromItems,
  vaultCapabilityKey,
  vaultCapabilitySnapshot,
} from "../../lib/vault-capabilities";
import type { VaultCapabilityDefinition, VaultCapabilityDraft, VaultCapabilityGrant } from "../../lib/vault-capabilities";
import { useRequestGuard } from "../../lib/request-guard";

export type VaultPermissionLoad = {
  tokenID: number | undefined;
  state: "idle" | "loading" | "ready" | "error";
  projects: TokenProjectScope[];
  definitions: VaultCapabilityDefinition[];
  capabilities: VaultCapabilityGrant[];
  scopeRevision: string;
  capabilityRevision: string;
  error: string | null;
};
export type VaultPermissionSave = { state: "idle" | "saving" | "ready" | "unsaved" | "error"; error: string | null };
type Guard = ReturnType<typeof useRequestGuard>;
type Request = ReturnType<Guard["begin"]>;
type CapabilityEdit = { draft: VaultCapabilityDraft; version: number };
type CommittedHandlers = {
  saveCapabilities: (_event: FormEvent<HTMLFormElement>) => Promise<void>;
  toggleProjectScope: (_projectID: number, _enabled: boolean) => Promise<void>;
  editCapability: (_update: SetStateAction<VaultCapabilityDraft>) => void;
};

const requestFailure = "Vault permission request failed.";

const emptyLoad: VaultPermissionLoad = {
  tokenID: undefined,
  state: "idle",
  projects: [],
  definitions: [],
  capabilities: [],
  scopeRevision: "",
  capabilityRevision: "",
  error: null,
};

export function useVaultPermissionEditor(tokenID: number | undefined, onSaved?: () => void | Promise<void>) {
  const [load, setLoad] = useState<VaultPermissionLoad>(emptyLoad);
  const [scopeDraft, setScopeDraft] = useState<Record<number, boolean>>({});
  const [capabilityEdit, setCapabilityEdit] = useState<CapabilityEdit>({ draft: {}, version: 0 });
  const capabilityDraft = capabilityEdit.draft;
  const [selectedProjectID, setSelectedProjectID] = useState(0);
  const [scopeSave, setScopeSave] = useState<VaultPermissionSave>({ state: "idle", error: null });
  const [save, setSave] = useState<VaultPermissionSave>({ state: "idle", error: null });
  const scopeInFlight = useRef<Request | null>(null);
  const capabilityInFlight = useRef<Request | null>(null);
  const draftVersion = useRef(0);
  const committedHandlers = useRef<CommittedHandlers | null>(null);
  const requests = useRequestGuard(`vault-permission-dialog:${tokenID || "closed"}`);
  const setCapabilityDraft = useCallback((draft: VaultCapabilityDraft) => {
    setCapabilityEdit((current) => ({ ...current, draft }));
  }, []);

  useEffect(() => {
    setLoad(emptyLoad);
    setScopeDraft({});
    setCapabilityEdit({ draft: {}, version: 0 });
    setSelectedProjectID(0);
    setScopeSave({ state: "idle", error: null });
    setSave({ state: "idle", error: null });
    scopeInFlight.current = null;
    capabilityInFlight.current = null;
    draftVersion.current = 0;
    if (tokenID) void loadVaultPermissionData({ tokenID, requests, setLoad, setScopeDraft, setCapabilityDraft });
  }, [tokenID, requests, setCapabilityDraft]);

  useEffect(() => {
    if (load.state !== "ready") return;
    if (selectedProjectID && load.projects.some((project) => project.project_id === selectedProjectID)) return;
    setSelectedProjectID(load.projects[0]?.project_id || 0);
  }, [load.state, load.projects, selectedProjectID]);

  const notifySaved = useCallback(
    async (request: Request, setStatus: Dispatch<SetStateAction<VaultPermissionSave>>, subject: string) => {
      try {
        await onSaved?.();
      } catch (error) {
        if (!request.isCurrent()) return;
        setStatus((current) => ({
          ...current,
          error: `${subject} saved, but refreshing token data failed: ${errorMessage(error, requestFailure)}`,
        }));
      }
    },
    [onSaved],
  );

  const toggleProjectScope = useCallback(
    async function toggleProjectScope(projectID: number, enabled: boolean) {
      if (committedHandlers.current?.toggleProjectScope !== toggleProjectScope) return;
      if (!tokenID || load.tokenID !== tokenID || load.state !== "ready" || scopeInFlight.current?.isCurrent()) return;
      requests.invalidate("load");
      const request = requests.begin("scope-save");
      scopeInFlight.current = request;
      const previousDraft = scopeDraft;
      const nextDraft = { ...scopeDraft, [projectID]: enabled };
      setScopeDraft(nextDraft);
      setScopeSave({ state: "saving", error: null });
      try {
        const projectsWithDraft = load.projects.map((project) => ({
          ...project,
          enabled: Boolean(nextDraft[project.project_id]),
        }));
        const result = await updateTokenProjectVisibility(tokenID, projectsWithDraft, projectID, enabled, {
          expectedRevision: load.scopeRevision,
          signal: request.signal,
        });
        if (!request.isCurrent()) return;
        const projects = result.items;
        committedHandlers.current = null;
        setLoad((current) => ({ ...current, projects, scopeRevision: result.revision }));
        setScopeDraft(Object.fromEntries(projects.map((project) => [project.project_id, Boolean(project.enabled)])));
        setScopeSave({ state: "ready", error: null });
      } catch (error) {
        if (!request.isCurrent()) return;
        setScopeDraft(previousDraft);
        setScopeSave({ state: "error", error: errorMessage(error, requestFailure) });
        return;
      } finally {
        request.complete();
        if (scopeInFlight.current === request) scopeInFlight.current = null;
      }
      await notifySaved(request, setScopeSave, "Project visibility");
    },
    [tokenID, load, scopeDraft, requests, notifySaved],
  );

  const editCapability = useCallback(
    function editCapability(update: SetStateAction<VaultCapabilityDraft>) {
      if (committedHandlers.current?.editCapability !== editCapability || !tokenID) return;
      // Every edit owns a new version, including edits that return to the submitted values.
      const version = ++draftVersion.current;
      setCapabilityEdit((current) => ({ draft: typeof update === "function" ? update(current.draft) : update, version }));
      setSave((current) => (current.state === "saving" ? current : { state: "idle", error: null }));
    },
    [tokenID],
  );

  const setCapabilityRule = useCallback(
    (projectID: number, capabilityName: string, executionRule: string) => {
      const key = vaultCapabilityKey(projectID, capabilityName);
      editCapability((current) => ({
        ...current,
        [key]: executionRule
          ? { execution_rule: executionRule, expires_at: current[key]?.expires_at || "" }
          : { execution_rule: "", expires_at: "" },
      }));
    },
    [editCapability],
  );

  const setCapabilityLifetime = useCallback(
    (projectID: number, capabilityName: string, lifetime: string) => {
      const key = vaultCapabilityKey(projectID, capabilityName);
      editCapability((current) => ({
        ...current,
        [key]: {
          execution_rule: current[key]?.execution_rule || "",
          expires_at: lifetime === "permanent" ? "" : expiresAtFromLifetime(lifetime),
        },
      }));
    },
    [editCapability],
  );

  const saveCapabilities = useCallback(
    async function saveCapabilities(event: FormEvent<HTMLFormElement>) {
      event.preventDefault();
      if (committedHandlers.current?.saveCapabilities !== saveCapabilities) return;
      if (!tokenID || load.tokenID !== tokenID || load.state !== "ready" || capabilityInFlight.current?.isCurrent()) return;
      requests.invalidate("load");
      const request = requests.begin("capability-save");
      capabilityInFlight.current = request;
      const submittedVersion = capabilityEdit.version;
      setSave({ state: "saving", error: null });
      try {
        const capabilities = vaultCapabilitiesFromDraft(load.projects, load.definitions, capabilityEdit.draft);
        const response = await apiPut(
          `/api/tokens/${tokenID}/project-capabilities`,
          { capabilities, expected_revision: load.capabilityRevision },
          { signal: request.signal },
        );
        if (!request.isCurrent()) return;
        const result = vaultCapabilitySnapshot(response);
        const { definitions, items } = result;
        committedHandlers.current = null;
        setLoad((current) => ({
          ...current,
          definitions,
          capabilities: items,
          capabilityRevision: result.revision,
        }));
        const unchanged = draftVersion.current === submittedVersion;
        if (unchanged) setCapabilityDraft(vaultCapabilityDraftFromItems(items, definitions));
        setSave({ state: unchanged ? "ready" : "unsaved", error: null });
      } catch (error) {
        if (!request.isCurrent()) return;
        setSave({ state: "error", error: errorMessage(error, requestFailure) });
        return;
      } finally {
        request.complete();
        if (capabilityInFlight.current === request) capabilityInFlight.current = null;
      }
      await notifySaved(request, setSave, "Vault capabilities");
    },
    [tokenID, load, capabilityEdit, requests, notifySaved, setCapabilityDraft],
  );

  useLayoutEffect(() => {
    // Only handlers for the committed snapshot may admit work; cleanup retires retained closures.
    requests.setScope(`vault-permission-dialog:${tokenID || "closed"}`);
    committedHandlers.current = { saveCapabilities, toggleProjectScope, editCapability };
    return () => {
      committedHandlers.current = null;
    };
  }, [tokenID, requests, saveCapabilities, toggleProjectScope, editCapability]);

  return {
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
  };
}

async function loadVaultPermissionData({
  tokenID,
  requests,
  setLoad,
  setScopeDraft,
  setCapabilityDraft,
}: {
  tokenID: number;
  requests: Guard;
  setLoad: Dispatch<SetStateAction<VaultPermissionLoad>>;
  setScopeDraft: Dispatch<SetStateAction<Record<number, boolean>>>;
  setCapabilityDraft: (_draft: VaultCapabilityDraft) => void;
}) {
  const request = requests.begin("load");
  setLoad((current) => ({ ...current, state: "loading", error: null }));
  try {
    const [projectScopes, projectCapabilities] = await Promise.all([
      apiGet(`/api/tokens/${tokenID}/project-scopes`, { signal: request.signal }),
      apiGet(`/api/tokens/${tokenID}/project-capabilities`, { signal: request.signal }),
    ]);
    if (!request.isCurrent()) return;
    const { items: projects, revision: scopeRevision } = tokenProjectScopeSnapshot(projectScopes);
    const { definitions, items: capabilities, revision: capabilityRevision } = vaultCapabilitySnapshot(projectCapabilities);
    setLoad({ tokenID, state: "ready", projects, definitions, capabilities, scopeRevision, capabilityRevision, error: null });
    setScopeDraft(Object.fromEntries(projects.map((project) => [project.project_id, Boolean(project.enabled)])));
    setCapabilityDraft(vaultCapabilityDraftFromItems(capabilities, definitions));
  } catch (error) {
    if (!request.isCurrent()) return;
    setLoad({ ...emptyLoad, state: "error", error: errorMessage(error, requestFailure) });
    setScopeDraft({});
    setCapabilityDraft({});
  } finally {
    request.complete();
  }
}
