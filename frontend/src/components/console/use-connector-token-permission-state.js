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
import { inferPermissionMode, tokenProfileModeKey } from "./connector-token-permission-model";

export function useConnectorTokenPermissionState({
  connectorPermissionState,
  loadAllConnectorPermissions,
  loadConnectorActions,
  onRefresh,
  replaceTokenConnectorPermissions,
  selectedTarget,
  targets,
  tokens,
}) {
  const activeTokens = useMemo(() => tokens.data.filter((token) => !token.revoked_at), [tokens.data]);
  const [savingKey, setSavingKey] = useState("");
  const [openTokenID, setOpenTokenID] = useState(null);
  const [profileByToken, setProfileByToken] = useState({});
  const [permissionModeByKey, setPermissionModeByKey] = useState({});
  const [projectScopesByToken, setProjectScopesByToken] = useState({});
  const [projectScopeRevisionByToken, setProjectScopeRevisionByToken] = useState({});
  const [projectScopeStateByToken, setProjectScopeStateByToken] = useState({});
  const [projectScopeError, setProjectScopeError] = useState("");
  const [permissionMutationError, setPermissionMutationError] = useState(null);
  const compactPanelRef = useRef(null);
  const tokenTriggerRef = useRef(null);
  const permissionMutationRetryRef = useRef(null);
  const permissionMutationActiveRef = useRef(false);
  const load = connectorPermissionState || { state: "idle", data: {}, actionsByTargetRef: {}, error: null };
  const permissionsByToken = useMemo(() => load.data || {}, [load.data]);
  const targetProfiles = useMemo(() => profilesForConnectorTarget(targets?.data || [], selectedTarget), [targets?.data, selectedTarget]);
  const selectedTargetKey = connectorTargetKey(selectedTarget);
  const tokenIDsKey = activeTokens.map((token) => token.id).join(",");
  const targetProfileSignature = targetProfiles.map((profile) => profile.profile_id).join(",");
  const loadForEffect = useEffectEvent(loadConnectorPermissions);
  const projectScopeRequests = useRequestGuard(`project-scopes:${selectedTargetKey}:${tokenIDsKey}`);

  useEffect(() => {
    setSavingKey("");
    setPermissionMutationError(null);
    permissionMutationRetryRef.current = null;
  }, [selectedTargetKey]);

  useEffect(() => {
    if (!selectedTarget) {
      setProfileByToken({});
      setProjectScopesByToken({});
      setProjectScopeRevisionByToken({});
      setProjectScopeStateByToken({});
      setPermissionMutationError(null);
      permissionMutationRetryRef.current = null;
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
    const closeOnOutsidePointer = (event) => !compactPanelRef.current?.contains(event.target) && setOpenTokenID(null);
    const closeOnEscape = (event) => {
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
    const result = {};
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
          setProjectScopesByToken((current) => ({ ...current, [token.id]: result.items || [] }));
          setProjectScopeRevisionByToken((current) => ({ ...current, [token.id]: result.revision || "" }));
          setProjectScopeStateByToken((current) => ({ ...current, [token.id]: "ready" }));
        } catch (error) {
          if (!request.isCurrent()) return;
          setProjectScopeStateByToken((current) => ({ ...current, [token.id]: "error" }));
          setProjectScopeError(error.message || "Failed to load token project scopes.");
        } finally {
          request.complete();
        }
      }),
    ]);
  }

  function projectEnabledForToken(tokenID) {
    const scope = (projectScopesByToken[tokenID] || []).find((item) => Number(item.project_id) === Number(selectedTarget?.project_id));
    return scope ? Boolean(scope.enabled) : false;
  }

  function projectScopeReadyForToken(tokenID) {
    return projectScopeStateByToken[tokenID] === "ready";
  }

  async function setProjectVisibility(token, enabled) {
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
      setProjectScopeError(error.message || "Failed to update token project scope.");
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

  function selectProfile(token, profileID) {
    const nextID = Number(profileID);
    if (!Number.isFinite(nextID) || nextID <= 0) return;
    setProfileByToken((current) => ({ ...current, [token.id]: nextID }));
    writeStoredConnectorProfileID(selectedTarget, token.id, nextID);
    void loadConnectorActions?.({ ...selectedTarget, profile_id: nextID });
  }

  async function setConnectorRules(token, profileID, selectedActions, rule, keySuffix) {
    if (!selectedTarget || permissionMutationActiveRef.current) return;
    await runPermissionMutation({
      key: `${token.id}:${profileID}:${keySuffix}`,
      retry: () => setConnectorRules(token, profileID, selectedActions, rule, keySuffix),
      failure: (error) =>
        createPermissionMutationFailure(token, profileID, selectedActions.map((action) => action.name).join(", "), error, {
          targetProfiles,
          selectedTargetKey,
        }),
      mutate: async () => {
        const existing = permissionsByToken[token.id] || [];
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
        const actions = load.actionsByTargetRef?.[connectorActionCacheKey(selectedTarget, profileID)] || selectedActions;
        const modeKey = tokenProfileModeKey(token.id, selectedTarget, profileID);
        const nextMode = inferPermissionMode(next, selectedTarget, profileID, actions);
        setPermissionModeByKey((current) => ({ ...current, [modeKey]: nextMode }));
        permissionMutationRetryRef.current = null;
        return next;
      },
    });
  }

  async function setProfileLifetime(token, profileID, expiresAt) {
    if (!selectedTarget || permissionMutationActiveRef.current) return;
    await runPermissionMutation({
      key: `${token.id}:${profileID}:lifetime`,
      retry: () => setProfileLifetime(token, profileID, expiresAt),
      failure: (error) => createPermissionMutationFailure(token, profileID, "lifetime", error, { targetProfiles, selectedTargetKey }),
      mutate: async () => {
        const existing = permissionsByToken[token.id] || [];
        const next = existing.map((permission) => {
          if (!matchesConnectorTargetProfile(permission, selectedTarget, profileID)) return permission;
          return effectiveRule(permission) === "blocked"
            ? { ...permission, expires_at: "" }
            : { ...permission, expires_at: expiresAt || "" };
        });
        await replaceTokenConnectorPermissions?.(token.id, next);
        permissionMutationRetryRef.current = null;
      },
    });
  }

  async function runPermissionMutation({ key, retry, failure, mutate }) {
    permissionMutationActiveRef.current = true;
    setSavingKey(key);
    setPermissionMutationError(null);
    permissionMutationRetryRef.current = retry;
    try {
      return await mutate();
    } catch (error) {
      setPermissionMutationError(failure(error));
      return null;
    } finally {
      permissionMutationActiveRef.current = false;
      setSavingKey("");
    }
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
    setConnectorRule: (token, profileID, action, rule) => setConnectorRules(token, profileID, [action], rule, action.name),
    setConnectorRules,
    setOpenTokenID,
    setPermissionModeByKey,
    setProfileLifetime,
    setProjectVisibility,
    targetProfiles,
    tokenTriggerRef,
  };
}

function createPermissionMutationFailure(token, profileID, operation, error, { targetProfiles, selectedTargetKey }) {
  const profile = targetProfiles.find((item) => Number(item.profile_id) === Number(profileID));
  return {
    tokenID: Number(token.id),
    profileID: Number(profileID),
    targetKey: selectedTargetKey,
    message: `${token.name} / ${profile?.profile_label || `profile ${profileID}`}: failed to update ${operation}. ${errorMessage(error, "Unknown error.")}`,
  };
}

function reconcileSelectedProfiles(current, activeTokens, target, profiles) {
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
