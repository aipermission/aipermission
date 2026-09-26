import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { apiGet } from "../../lib/api";
import {
  connectorTargetKey,
  connectorTargetProfileLifetime,
  currentConnectorTargetProfilePermissions,
  matchesConnectorTargetProfile,
  profilesForConnectorTarget,
  readStoredConnectorProfileID,
  selectedConnectorProfileID,
  writeStoredConnectorProfileID,
} from "../../lib/connector-permissions";
import { errorMessage } from "../../lib/errors";
import { effectiveRule } from "../../lib/permissions";
import { updateTokenProjectVisibility } from "../../lib/project-scopes";
import { connectorActionCacheKey } from "../../lib/use-connector-permissions";
import { useRequestGuard } from "../../lib/request-guard";
import { inferPermissionMode, tokenProfileModeKey, type PermissionMode } from "./connector-token-permission-model";
import { isActiveToken } from "../../lib/token-status";
import { useTokenExpiryClock } from "../../lib/use-token-expiry-clock";
import { tokenProjectScopeSnapshot } from "../../lib/gateway-contracts/security-contracts";
import type { Dispatch, RefObject, SetStateAction } from "react";
import type { GatewayToken, GatewayTarget } from "../../lib/gateway-contracts/core-resource-contracts";
import type { ExecutionRule, TokenActionPermission, TokenProjectScope } from "../../lib/gateway-contracts/security-contracts";
import type { useConnectorPermissions, PermissionState, ConnectorPermissionAction } from "../../lib/use-connector-permissions";

export type PermissionTarget = Pick<GatewayTarget, "connector_kind" | "target_id" | "profile_id"> &
  Partial<Pick<GatewayTarget, "id" | "project_id" | "profile_label" | "target_name" | "project_name" | "runtime_id">>;
type Permissions = Record<number, TokenActionPermission[]>;
type Owner = { targetKey: string };
type Retry = () => Promise<void>;
export type PermissionMutationError = { tokenID: number; profileID: number; targetKey: string; message: string; retryable?: boolean };
type Mutation = {
  key: string; retry: Retry; failure: (_error: unknown) => PermissionMutationError;
  mutate: (_isCurrent: () => boolean) => Promise<unknown>;
};
type MutationControls = {
  permissionMutationActiveRef: RefObject<boolean>; permissionMutationOwnerRef: RefObject<Owner | null>;
  permissionMutationRetryRef: RefObject<Retry | null>; setPermissionMutationError: Dispatch<SetStateAction<PermissionMutationError | null>>;
  setSavingKey: Dispatch<SetStateAction<string>>;
};
export type ConnectorTokenPermissionOptions = Partial<Pick<ReturnType<typeof useConnectorPermissions>, "connectorPermissionState" | "loadAllConnectorPermissions" | "loadConnectorActions" | "replaceTokenConnectorPermissions">> & {
  onRefresh?: () => unknown | Promise<unknown>; selectedTarget: PermissionTarget | null;
  targets?: { data: PermissionTarget[] }; tokens: { data: GatewayToken[] };
};

export function useConnectorTokenPermissionState({
  connectorPermissionState,
  loadAllConnectorPermissions,
  loadConnectorActions,
  onRefresh,
  replaceTokenConnectorPermissions,
  selectedTarget,
  targets,
  tokens,
}: ConnectorTokenPermissionOptions) {
  const activeTokens = useActiveTokens(tokens.data);
  const [savingKey, setSavingKey] = useState("");
  const [openTokenID, setOpenTokenID] = useState<number | null>(null);
  const [profileByToken, setProfileByToken] = useState<Record<number, number | "">>({});
  const [permissionModeByKey, setPermissionModeByKey] = useState<Record<string, PermissionMode>>({});
  const [projectScopesByToken, setProjectScopesByToken] = useState<Record<number, TokenProjectScope[]>>({});
  const [projectScopeRevisionByToken, setProjectScopeRevisionByToken] = useState<Record<number, string>>({});
  const [projectScopeStateByToken, setProjectScopeStateByToken] = useState<Record<number, string>>({});
  const [projectScopeError, setProjectScopeError] = useState("");
  const [permissionMutationError, setPermissionMutationError] = useState<PermissionMutationError | null>(null);
  const compactPanelRef = useRef<HTMLElement | null>(null);
  const tokenTriggerRef = useRef<HTMLButtonElement | null>(null);
  const permissionMutationRetryRef = useRef<Retry | null>(null);
  const permissionMutationActiveRef = useRef(false);
  const permissionMutationOwnerRef = useRef<Owner | null>(null);
  const load: PermissionState = connectorPermissionState || { state: "idle", data: {}, revisionsByToken: {}, actionsByTargetRef: {}, error: null };
  const permissionsByToken = useMemo(() => load.data || {}, [load.data]);
  const permissionsByTokenRef = useRef(permissionsByToken);
  const permissionSnapshotSourceRef = useRef(permissionsByToken);
  if (permissionSnapshotSourceRef.current !== permissionsByToken) {
    permissionSnapshotSourceRef.current = permissionsByToken;
    permissionsByTokenRef.current = permissionsByToken;
  }
  const targetProfiles = useMemo(() => profilesForConnectorTarget(targets?.data || [], selectedTarget), [targets?.data, selectedTarget]);
  const selectedTargetKey = connectorTargetKey(selectedTarget);
  const permissionMutationScopeRef = useRef(selectedTargetKey);
  if (permissionMutationScopeRef.current !== selectedTargetKey) {
    permissionMutationScopeRef.current = selectedTargetKey;
    permissionMutationOwnerRef.current = null;
    permissionMutationActiveRef.current = false;
    permissionMutationRetryRef.current = null;
  }
  const tokenIDsKey = activeTokens.map((token) => token.id).join(",");
  const targetProfileSignature = targetProfiles.map((profile) => profile.profile_id).join(",");
  const loadForEffect = useEffectEvent(loadConnectorPermissions);
  const projectScopeRequests = useRequestGuard(`project-scopes:${selectedTargetKey}:${tokenIDsKey}`);

  useEffect(() => {
    setSavingKey("");
    setPermissionMutationError(null);
  }, [selectedTargetKey]);

  useEffect(() => {
    if (!selectedTarget) {
      setProfileByToken({});
      setProjectScopesByToken({});
      setProjectScopeRevisionByToken({});
      setProjectScopeStateByToken({});
      return;
    }
    void loadForEffect();
  }, [selectedTarget, targetProfileSignature, tokenIDsKey]);

  useEffect(() => {
    if (!selectedTarget || targetProfiles.length === 0) return;
    setProfileByToken((current) => reconcileSelectedProfiles(current, activeTokens, selectedTarget, targetProfiles));
  }, [selectedTarget, targetProfiles, activeTokens]);

  useEffect(() => {
    if (!openTokenID) return undefined;
    const closeOnOutsidePointer = (event: PointerEvent) => event.target instanceof Node && !compactPanelRef.current?.contains(event.target) && setOpenTokenID(null);
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      setOpenTokenID(null);
      queueMicrotask(() => tokenTriggerRef.current?.focus());
    };
    window.addEventListener("pointerdown", closeOnOutsidePointer);
    window.addEventListener("keydown", closeOnEscape);
    return () => {
      window.removeEventListener("pointerdown", closeOnOutsidePointer);
      window.removeEventListener("keydown", closeOnEscape);
    };
  }, [openTokenID]);

  const selectedCountByToken = useMemo(() => {
    const result: Record<number, number> = {};
    for (const token of activeTokens) {
      const profileID = selectedConnectorProfileID(token.id, selectedTarget, targetProfiles, profileByToken);
      result[token.id] = currentConnectorTargetProfilePermissions(permissionsByToken[token.id] || [], selectedTarget, profileID).length;
    }
    return result;
  }, [activeTokens, permissionsByToken, selectedTarget, targetProfiles, profileByToken]);

  async function loadConnectorPermissions() {
    if (!selectedTarget) return;
    setProjectScopeError("");
    const profilesToLoad = targetProfiles.length > 0 ? targetProfiles : [selectedTarget];
    await Promise.all([
      ...profilesToLoad.map((profile) => loadConnectorActions?.({ ...selectedTarget, profile_id: profile.profile_id || profile.id })),
      loadAllConnectorPermissions?.(activeTokens),
      ...activeTokens.map(async (token) => {
        const request = projectScopeRequests.begin(`token:${token.id}`);
        setProjectScopeStateByToken((current) => ({ ...current, [token.id]: "loading" }));
        try {
          const result = await apiGet(`/api/tokens/${token.id}/project-scopes`, { signal: request.signal });
          if (!request.isCurrent()) return;
          const snapshot = tokenProjectScopeSnapshot(result);
          setProjectScopesByToken((current) => ({ ...current, [token.id]: snapshot.items }));
          setProjectScopeRevisionByToken((current) => ({ ...current, [token.id]: snapshot.revision }));
          setProjectScopeStateByToken((current) => ({ ...current, [token.id]: "ready" }));
        } catch (error) {
          if (!request.isCurrent()) return;
          setProjectScopeStateByToken((current) => ({ ...current, [token.id]: "error" }));
          setProjectScopeError(errorMessage(error, "Failed to load token project scopes."));
        } finally {
          request.complete();
        }
      }),
    ]);
  }

  const projectEnabledForToken = (tokenID: number) =>
    (projectScopesByToken[tokenID] || []).some(
      (item) => Number(item.project_id) === Number(selectedTarget?.project_id) && Boolean(item.enabled),
    );

  const projectScopeReadyForToken = (tokenID: number) => projectScopeStateByToken[tokenID] === "ready";

  async function setProjectVisibility(token: GatewayToken, enabled: boolean) {
    if (!selectedTarget?.project_id || !projectScopeReadyForToken(token.id)) return;
    const request = projectScopeRequests.begin(`token:${token.id}`);
    setSavingKey(`${token.id}:project:${selectedTarget.project_id}`);
    setProjectScopeStateByToken((current) => ({ ...current, [token.id]: "saving" }));
    setProjectScopeError("");
    try {
      const scopes = projectScopesByToken[token.id] || [];
      const result = await updateTokenProjectVisibility(token.id, scopes, selectedTarget.project_id, enabled, {
        expectedRevision: projectScopeRevisionByToken[token.id],
        signal: request.signal,
      });
      if (!request.isCurrent()) return;
      setProjectScopesByToken((current) => ({ ...current, [token.id]: result.items || [] }));
      setProjectScopeRevisionByToken((current) => ({ ...current, [token.id]: result.revision || "" }));
      setProjectScopeStateByToken((current) => ({ ...current, [token.id]: "ready" }));
      await loadAllConnectorPermissions?.(activeTokens);
    } catch (error) {
      if (!request.isCurrent()) return;
      setProjectScopeStateByToken((current) => ({ ...current, [token.id]: "ready" }));
      setProjectScopeError(errorMessage(error, "Failed to update token project scope."));
    } finally {
      if (request.isCurrent()) setSavingKey("");
      request.complete();
    }
  }

  async function refreshPanel() {
    if (savingKey) return;
    await onRefresh?.();
    await loadConnectorPermissions();
  }

  function selectProfile(token: GatewayToken, profileID: number | string) {
    const nextID = Number(profileID);
    if (!Number.isFinite(nextID) || nextID <= 0) return;
    setProfileByToken((current) => ({ ...current, [token.id]: nextID }));
    writeStoredConnectorProfileID(selectedTarget, token.id, nextID);
    void loadConnectorActions?.({ ...selectedTarget, profile_id: nextID });
  }

  async function setConnectorRules(token: GatewayToken, profileID: number, selectedActions: ConnectorPermissionAction[], rule: ExecutionRule | "", keySuffix: string) {
    if (!selectedTarget || permissionMutationActiveRef.current) return;
    await runPermissionMutation({
      key: `${token.id}:${profileID}:${keySuffix}`,
      retry: () => setConnectorRules(token, profileID, selectedActions, rule, keySuffix),
      failure: (error) =>
        createPermissionMutationFailure(token, profileID, selectedActions.map((action) => action.name).join(", "), error, {
          targetProfiles,
          selectedTargetKey,
        }),
      mutate: async (isCurrent) => {
        const existing = permissionsByTokenRef.current[token.id] || [];
        const actionNames = new Set(selectedActions.map((action) => action.name));
        const preserved = existing.filter(
          (permission) => !matchesConnectorTargetProfile(permission, selectedTarget, profileID) || !actionNames.has(permission.action_name),
        );
        const expiresAt = rule === "blocked" ? "" : connectorTargetProfileLifetime(existing, selectedTarget, profileID)?.expires_at || "";
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
        if (!isCurrent()) return next;
        const actions = load.actionsByTargetRef?.[connectorActionCacheKey(selectedTarget, profileID)] || selectedActions;
        const modeKey = tokenProfileModeKey(token.id, selectedTarget, profileID);
        const nextMode = inferPermissionMode(next, selectedTarget, profileID, actions);
        setPermissionModeByKey((current) => ({ ...current, [modeKey]: nextMode }));
        return next;
      },
    });
  }

  async function setProfileLifetime(token: GatewayToken, profileID: number, expiresAt: string) {
    if (!selectedTarget || permissionMutationActiveRef.current) return;
    await runPermissionMutation({
      key: `${token.id}:${profileID}:lifetime`,
      retry: () => setProfileLifetime(token, profileID, expiresAt),
      failure: (error) => createPermissionMutationFailure(token, profileID, "lifetime", error, { targetProfiles, selectedTargetKey }),
      mutate: async () => {
        const existing = permissionsByTokenRef.current[token.id] || [];
        const next = existing.map((permission) => {
          if (!matchesConnectorTargetProfile(permission, selectedTarget, profileID)) return permission;
          return effectiveRule(permission) === "blocked"
            ? { ...permission, expires_at: "" }
            : { ...permission, expires_at: expiresAt || "" };
        });
        await replaceTokenConnectorPermissions?.(token.id, next);
      },
    });
  }

  async function runPermissionMutation({ key, retry, failure, mutate }: Mutation) {
    return executePermissionMutation(
      { key, retry, failure, mutate, owner: { targetKey: selectedTargetKey } },
      { permissionMutationActiveRef, permissionMutationOwnerRef, permissionMutationRetryRef, setPermissionMutationError, setSavingKey },
      (error) => refreshPermissionSnapshot(error, loadAllConnectorPermissions, activeTokens, permissionsByTokenRef),
    );
  }

  return {
    activeTokens,
    compactPanelRef,
    load,
    openTokenID,
    permissionModeByKey,
    permissionMutationError,
    permissionsByToken,
    profileByToken,
    projectEnabledForToken,
    projectScopeReadyForToken,
    projectScopeError,
    refreshPanel,
    retryPermissionMutation: () => permissionMutationRetryRef.current?.(),
    savingKey,
    selectProfile,
    selectedCountByToken,
    selectedTargetKey,
    setConnectorRule: (token: GatewayToken, profileID: number, action: ConnectorPermissionAction, rule: ExecutionRule | "") => setConnectorRules(token, profileID, [action], rule, action.name),
    setConnectorRules,
    setOpenTokenID,
    setPermissionModeByKey,
    setProfileLifetime,
    setProjectVisibility,
    targetProfiles,
    tokenTriggerRef,
  };
}

function useActiveTokens(tokens: GatewayToken[]) {
  const now = useTokenExpiryClock(tokens);
  return useMemo(() => tokens.filter((token) => isActiveToken(token, now)), [tokens, now]);
}

async function refreshPermissionSnapshot(error: unknown, loadAllConnectorPermissions: ConnectorTokenPermissionOptions["loadAllConnectorPermissions"], activeTokens: GatewayToken[], permissionsByTokenRef: RefObject<Permissions>) {
  if (!error || typeof error !== "object" || !("status" in error) || error.status !== 409) return;
  const refreshed = await loadAllConnectorPermissions?.(activeTokens, { requireCurrent: true });
  const completeSnapshot = refreshed && typeof refreshed === "object" && activeTokens.every((token) => Array.isArray(refreshed[token.id]));
  if (!completeSnapshot || !refreshed) throw new Error("Current connector permissions could not be refreshed; retry is disabled.");
  permissionsByTokenRef.current = refreshed;
}

async function executePermissionMutation(
  { key, retry, failure, mutate, owner }: Mutation & { owner: Owner },
  { permissionMutationActiveRef, permissionMutationOwnerRef, permissionMutationRetryRef, setPermissionMutationError, setSavingKey }: MutationControls,
  refreshConflict: (_error: unknown) => Promise<void>,
) {
  const isCurrent = () => permissionMutationOwnerRef.current === owner;
  permissionMutationOwnerRef.current = owner;
  permissionMutationActiveRef.current = true;
  setSavingKey(key);
  setPermissionMutationError(null);
  permissionMutationRetryRef.current = retry;
  try {
    const result = await mutate(isCurrent);
    if (isCurrent()) permissionMutationRetryRef.current = null;
    return result;
  } catch (error) {
    if (!isCurrent()) return null;
    try {
      await refreshConflict(error);
      if (!isCurrent()) return null;
      setPermissionMutationError({ ...failure(error), retryable: true });
    } catch (refreshError) {
      if (!isCurrent()) return null;
      permissionMutationRetryRef.current = null;
      setPermissionMutationError({ ...failure(refreshError), retryable: false });
    }
    return null;
  } finally {
    if (isCurrent()) {
      permissionMutationOwnerRef.current = null;
      permissionMutationActiveRef.current = false;
      setSavingKey("");
    }
  }
}

function createPermissionMutationFailure(token: GatewayToken, profileID: number, operation: string, error: unknown, { targetProfiles, selectedTargetKey }: { targetProfiles: PermissionTarget[]; selectedTargetKey: string }): PermissionMutationError {
  const profile = targetProfiles.find((item) => Number(item.profile_id) === Number(profileID));
  return {
    tokenID: Number(token.id),
    profileID: Number(profileID),
    targetKey: selectedTargetKey,
    message: `${token.name} / ${profile?.profile_label || `profile ${profileID}`}: failed to update ${operation}. ${errorMessage(error, "Unknown error.")}`,
  };
}

function reconcileSelectedProfiles(current: Record<number, number | "">, activeTokens: GatewayToken[], target: PermissionTarget, profiles: PermissionTarget[]): Record<number, number | ""> {
  const next = { ...current };
  let changed = false;
  for (const token of activeTokens) {
    const stored = readStoredConnectorProfileID(target, token.id);
    const fallbackID = target.profile_id || (profiles.length === 1 ? profiles[0].profile_id : "");
    const currentID = current[token.id] || stored || fallbackID;
    const valid = profiles.some((profile) => Number(profile.profile_id) === Number(currentID));
    const nextID = valid ? Number(currentID) : fallbackID ? Number(fallbackID) : "";
    if (String(next[token.id] || "") !== String(nextID || "")) {
      next[token.id] = nextID;
      changed = true;
    }
  }
  return changed ? next : current;
}
