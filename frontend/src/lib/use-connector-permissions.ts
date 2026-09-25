import { useCallback, useRef, useState } from "react";
import { apiGet, apiPut } from "./api";
import { useRequestGuard } from "./request-guard";
import { tokenActionPermissionSnapshot } from "./gateway-contracts/security-contracts";
import type { TokenActionPermission } from "./gateway-contracts/security-contracts";
import type { components } from "../../types/generated-openapi";

type Token = { id: number };
type Target = {
  id?: number;
  target_id?: number;
  connector_kind?: string;
  profile_id?: number;
  profiles?: { id: number }[];
};
type Action = { name: string; [field: string]: unknown };
type PermissionInput = components["schemas"]["ConnectorPermissionInput"];

type PermissionState = {
  state: "idle" | "loading" | "ready" | "error";
  data: Record<number, TokenActionPermission[]>;
  revisionsByToken: Record<number, string>;
  actionsByTargetRef: Record<string, Action[]>;
  error: string | null;
};

const emptyState: PermissionState = {
  state: "idle",
  data: {},
  revisionsByToken: {},
  actionsByTargetRef: {},
  error: null,
};

export function useConnectorPermissions(initialTokens: Token[] = []) {
  const [permissionState, setPermissionState] = useState<PermissionState>(emptyState);
  const permissionRevisionRef = useRef(0);
  const serverRevisionsRef = useRef<Record<number, string>>({});
  const requestGuard = useRequestGuard("connector-permissions");

  const loadAllConnectorPermissions = useCallback(
    async (tokenItems: Token[] = initialTokens, { requireCurrent = false }: { requireCurrent?: boolean } = {}) => {
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
            const response = await apiGet(`/api/tokens/${token.id}/connector-permissions`, { signal: request.signal });
            if (!request.isCurrent() || revision !== permissionRevisionRef.current)
              return [token.id, [] as TokenActionPermission[], ""] as const;
            const permissions = tokenActionPermissionSnapshot(response);
            return [token.id, permissions.items, permissions.revision] as const;
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
        setPermissionState((current) => ({
          ...current,
          state: "error",
          error: error instanceof Error ? error.message : "Failed to load connector permissions.",
        }));
        if (requireCurrent) throw error;
        return {};
      } finally {
        request.complete();
      }
    },
    [initialTokens, requestGuard],
  );

  const loadConnectorActions = useCallback(
    async (targetOrKind: Target | string | null | undefined): Promise<Action[]> => {
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
          if (!request.isCurrent()) return [];
          const actions = connectorActions(result);
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
        setPermissionState((current) => ({
          ...current,
          state: "error",
          error: error instanceof Error ? error.message : "Failed to load connector actions.",
        }));
        return [];
      } finally {
        request?.complete();
      }
    },
    [requestGuard],
  );

  const replaceTokenConnectorPermissions = useCallback(
    async (tokenID: number, permissions: PermissionInput[]) => {
      permissionRevisionRef.current += 1;
      requestGuard.invalidate("permissions:load");
      const request = requestGuard.begin(`permissions:write:${tokenID}`);
      try {
        const expectedRevision = serverRevisionsRef.current[tokenID] || "";
        const response = await apiPut(
          `/api/tokens/${tokenID}/connector-permissions`,
          { permissions: permissions.map(permissionInput), expected_revision: expectedRevision },
          { signal: request.signal },
        );
        if (!request.isCurrent()) return [];
        const result = tokenActionPermissionSnapshot(response);
        const items = result.items;
        serverRevisionsRef.current = { ...serverRevisionsRef.current, [tokenID]: result.revision };
        setPermissionState((current) => ({
          ...current,
          state: "ready",
          data: {
            ...current.data,
            [tokenID]: items,
          },
          revisionsByToken: {
            ...current.revisionsByToken,
            [tokenID]: result.revision,
          },
          error: null,
        }));
        return items;
      } catch (error) {
        if (!request.isCurrent()) return [];
        setPermissionState((current) => ({
          ...current,
          state: "error",
          error: error instanceof Error ? error.message : "Failed to update connector permissions.",
        }));
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

export function connectorActionCacheKey(target: Target | null | undefined, profileID: number | string | null | undefined): string {
  const targetID = target?.target_id || target?.id || "";
  const kind = target?.connector_kind || "connector";
  return `${kind}:${targetID}:${profileID || ""}`;
}

function permissionInput(permission: PermissionInput): PermissionInput {
  return {
    target_id: permission.target_id,
    profile_id: permission.profile_id,
    action_name: permission.action_name,
    execution_rule: permission.execution_rule,
    expires_at: permission.expires_at || undefined,
  };
}

function connectorActions(value: unknown): Action[] {
  if (!value || typeof value !== "object" || Array.isArray(value) || !Array.isArray((value as { items?: unknown }).items)) {
    throw new Error("Invalid connector action catalog from gateway.");
  }
  return (value as { items: unknown[] }).items.map((entry) => {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
      throw new Error("Invalid connector action catalog from gateway.");
    }
    const name = (entry as { name?: unknown }).name;
    if (typeof name !== "string" || name.length === 0) {
      throw new Error("Invalid connector action catalog from gateway.");
    }
    return entry as Action;
  });
}
