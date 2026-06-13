import { useState } from "react";
import { apiGet, apiPut } from "./api";

const emptyState = {
  state: "idle",
  data: {},
  actionsByTargetRef: {},
  error: null,
};

export function useConnectorPermissions(initialTokens = []) {
  const [permissionState, setPermissionState] = useState(emptyState);

  async function loadAllConnectorPermissions(tokenItems = initialTokens) {
    if (tokenItems.length === 0) {
      setPermissionState((current) => ({ ...current, state: "ready", data: {}, error: null }));
      return {};
    }
    setPermissionState((current) => ({ ...current, state: "loading", error: null }));
    try {
      const entries = await Promise.all(
        tokenItems.map(async (token) => {
          const permissions = await apiGet(`/api/tokens/${token.id}/connector-permissions`);
          return [token.id, permissions.items || []];
        })
      );
      const data = Object.fromEntries(entries);
      setPermissionState((current) => ({ ...current, state: "ready", data, error: null }));
      return data;
    } catch (error) {
      setPermissionState((current) => ({ ...current, state: "error", error: error.message }));
      return {};
    }
  }

  async function loadConnectorActions(targetOrKind) {
    if (!targetOrKind) return [];
    setPermissionState((current) => ({ ...current, error: null }));
    try {
      if (typeof targetOrKind === "object") {
        const target = targetOrKind;
        const targetID = target.target_id || target.id;
        const profileID = target.profile_id || (target.profiles?.length === 1 ? target.profiles[0]?.id : "");
        if (!targetID || !profileID) return [];
        const result = await apiGet(`/api/connector-targets/${targetID}/profiles/${profileID}/actions`);
        const actions = result.items || [];
        const cacheKey = connectorActionCacheKey(target, profileID);
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
      setPermissionState((current) => ({ ...current, state: "error", error: error.message }));
      return [];
    }
  }

  async function replaceTokenConnectorPermissions(tokenID, permissions) {
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
  }

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
