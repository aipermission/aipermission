import { useCallback, useRef, useState } from "react";
import { apiGet, apiPut } from "./api";
import { useRequestGuard } from "./request-guard";

const emptyState = {
  state: "idle",
  data: {},
  revisionsByToken: {},
  actionsByTargetRef: {},
  error: null,
};

export function useConnectorPermissions(initialTokens = []) {
  const [permissionState, setPermissionState] = useState(emptyState);
  const permissionRevisionRef = useRef(0);
  const serverRevisionsRef = useRef({});
  const requestGuard = useRequestGuard("connector-permissions");

  const loadAllConnectorPermissions = useCallback(
    async (tokenItems = initialTokens, { requireCurrent = false } = {}) => {
      const request = requestGuard.begin("permissions:load");
      const revision = permissionRevisionRef.current;
      if (tokenItems.length === 0) {
        if (request.isCurrent()) {
          serverRevisionsRef.current = {};
          setPermissionState((current) => ({ ...current, state: "ready", data: {}, revisionsByToken: {}, error: null }));
        }
        request.complete();
        return {};
      }
      setPermissionState((current) => ({ ...current, state: "loading", error: null }));
      try {
        const entries = await Promise.all(
          tokenItems.map(async (token) => {
            const permissions = await apiGet(`/api/tokens/${token.id}/connector-permissions`, { signal: request.signal });
            return [token.id, permissions.items || [], permissions.revision || ""];
          }),
        );
        const data = Object.fromEntries(entries.map(([tokenID, items]) => [tokenID, items]));
        const revisionsByToken = Object.fromEntries(entries.map(([tokenID, _items, revision]) => [tokenID, revision]));
        if (!request.isCurrent() || revision !== permissionRevisionRef.current) {
          if (requireCurrent) throw new Error("Permission refresh was superseded before it could be applied.");
          return data;
        }
        serverRevisionsRef.current = revisionsByToken;
        setPermissionState((current) => ({ ...current, state: "ready", data, revisionsByToken, error: null }));
        return data;
      } catch (error) {
        if (!request.isCurrent() || revision !== permissionRevisionRef.current) {
          if (requireCurrent) throw error;
          return {};
        }
        setPermissionState((current) => ({ ...current, state: "error", error: error.message }));
        if (requireCurrent) throw error;
        return {};
      } finally {
        request.complete();
      }
    },
    [initialTokens, requestGuard],
  );

  const loadConnectorActions = useCallback(
    async (targetOrKind) => {
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
          request = requestGuard.begin(`actions:${cacheKey}`);
          const result = await apiGet(`/api/connector-targets/${targetID}/profiles/${profileID}/actions`, { signal: request.signal });
          const actions = result.items || [];
          if (!request.isCurrent()) return actions;
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
        if (request && !request.isCurrent()) return [];
        setPermissionState((current) => ({ ...current, state: "error", error: error.message }));
        return [];
      } finally {
        request?.complete();
      }
    },
    [requestGuard],
  );

  const replaceTokenConnectorPermissions = useCallback(
    async (tokenID, permissions) => {
      permissionRevisionRef.current += 1;
      requestGuard.invalidate("permissions:load");
      const request = requestGuard.begin(`permissions:write:${tokenID}`);
      try {
        const expectedRevision = serverRevisionsRef.current[tokenID] || "";
        const result = await apiPut(
          `/api/tokens/${tokenID}/connector-permissions`,
          { permissions: permissions.map(permissionInput), expected_revision: expectedRevision },
          { signal: request.signal },
        );
        const items = result.items || [];
        if (!request.isCurrent()) return items;
        serverRevisionsRef.current = { ...serverRevisionsRef.current, [tokenID]: result.revision || "" };
        setPermissionState((current) => ({
          ...current,
          state: "ready",
          data: {
            ...current.data,
            [tokenID]: items,
          },
          revisionsByToken: {
            ...current.revisionsByToken,
            [tokenID]: result.revision || "",
          },
          error: null,
        }));
        return items;
      } catch (error) {
        if (!request.isCurrent()) return [];
        setPermissionState((current) => ({ ...current, state: "error", error: error.message }));
        throw error;
      } finally {
        if (request.isCurrent()) {
          permissionRevisionRef.current += 1;
          requestGuard.invalidate("permissions:load");
        }
        request.complete();
      }
    },
    [requestGuard],
  );

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
