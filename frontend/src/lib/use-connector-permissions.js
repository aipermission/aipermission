import { useCallback, useEffect, useRef, useState } from "react";
import { apiGet, apiPut } from "./api";

const emptyState = {
  state: "idle",
  data: {},
  actionsByTargetRef: {},
  error: null,
};

export function useConnectorPermissions(initialTokens = []) {
  const [permissionState, setPermissionState] = useState(emptyState);
  const mountedRef = useRef(false);
  const permissionLoadRef = useRef(0);
  const actionLoadRefs = useRef(new Map());

  useEffect(() => {
    const actionLoads = actionLoadRefs.current;
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      permissionLoadRef.current += 1;
      actionLoads.clear();
    };
  }, []);

  const loadAllConnectorPermissions = useCallback(
    async (tokenItems = initialTokens) => {
      const generation = ++permissionLoadRef.current;
      if (tokenItems.length === 0) {
        if (mountedRef.current) setPermissionState((current) => ({ ...current, state: "ready", data: {}, error: null }));
        return {};
      }
      setPermissionState((current) => ({ ...current, state: "loading", error: null }));
      try {
        const entries = await Promise.all(
          tokenItems.map(async (token) => {
            const permissions = await apiGet(`/api/tokens/${token.id}/connector-permissions`);
            return [token.id, permissions.items || []];
          }),
        );
        const data = Object.fromEntries(entries);
        if (!mountedRef.current || generation !== permissionLoadRef.current) return data;
        setPermissionState((current) => ({ ...current, state: "ready", data, error: null }));
        return data;
      } catch (error) {
        if (!mountedRef.current || generation !== permissionLoadRef.current) return {};
        setPermissionState((current) => ({ ...current, state: "error", error: error.message }));
        return {};
      }
    },
    [initialTokens],
  );

  const loadConnectorActions = useCallback(async (targetOrKind) => {
    if (!targetOrKind) return [];
    setPermissionState((current) => ({ ...current, error: null }));
    let request = null;
    try {
      if (typeof targetOrKind === "object") {
        const target = targetOrKind;
        const targetID = target.target_id || target.id;
        const profileID = target.profile_id || (target.profiles?.length === 1 ? target.profiles[0]?.id : "");
        if (!targetID || !profileID) return [];
        const cacheKey = connectorActionCacheKey(target, profileID);
        const generation = (actionLoadRefs.current.get(cacheKey) || 0) + 1;
        actionLoadRefs.current.set(cacheKey, generation);
        request = { cacheKey, generation };
        const result = await apiGet(`/api/connector-targets/${targetID}/profiles/${profileID}/actions`);
        const actions = result.items || [];
        if (!mountedRef.current || actionLoadRefs.current.get(cacheKey) !== generation) return actions;
        setPermissionState((current) => ({
          ...current,
          actionsByTargetRef: {
            ...current.actionsByTargetRef,
            [cacheKey]: actions,
          },
          error: null,
        }));
        return actions;
      }
      return [];
    } catch (error) {
      if (!mountedRef.current || (request && actionLoadRefs.current.get(request.cacheKey) !== request.generation)) return [];
      setPermissionState((current) => ({ ...current, state: "error", error: error.message }));
      return [];
    }
  }, []);

  const replaceTokenConnectorPermissions = useCallback(async (tokenID, permissions) => {
    try {
      const result = await apiPut(`/api/tokens/${tokenID}/connector-permissions`, {
        permissions: permissions.map(permissionInput),
      });
      setPermissionState((current) => ({
        ...current,
        state: "ready",
        data: {
          ...current.data,
          [tokenID]: result.items || [],
        },
        error: null,
      }));
      return result.items || [];
    } catch (error) {
      setPermissionState((current) => ({ ...current, state: "error", error: error.message }));
      throw error;
    }
  }, []);

  return {
    connectorPermissionState: permissionState,
    loadAllConnectorPermissions,
    loadConnectorActions,
    replaceTokenConnectorPermissions,
  };
}

export function connectorActionCacheKey(target, profileID) {
  const targetID = target?.target_id || target?.id || "";
  const kind = target?.connector_kind || "connector";
  return `${kind}:${targetID}:${profileID || ""}`;
}

function permissionInput(permission) {
  return {
    target_id: permission.target_id,
    profile_id: permission.profile_id,
    action_name: permission.action_name,
    execution_rule: permission.execution_rule,
    expires_at: permission.expires_at || undefined,
  };
}
