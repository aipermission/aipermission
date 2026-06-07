import { useState } from "react";
import { apiGet, apiPut } from "./api";
import { effectiveRule, normalizePermission, permissionExpired, permissionsToMap } from "./permissions";

export function useTokenPermissions(initialTokens = []) {
  const [permissionState, setPermissionState] = useState({ state: "idle", data: {}, error: null, savingKey: "" });

  async function loadAllTokenPermissions(tokenItems = initialTokens) {
    if (tokenItems.length === 0) {
      setPermissionState({ state: "ready", data: {}, error: null, savingKey: "" });
      return;
    }
    setPermissionState((current) => ({ ...current, state: "loading", error: null }));
    try {
      const entries = await Promise.all(
        tokenItems.map(async (token) => {
          const permissions = await apiGet(`/api/tokens/${token.id}/permissions`);
          return [token.id, permissionsToMap(permissions)];
        })
      );
      setPermissionState({ state: "ready", data: Object.fromEntries(entries), error: null, savingKey: "" });
    } catch (error) {
      setPermissionState((current) => ({ ...current, state: "error", error: error.message, savingKey: "" }));
    }
  }

  async function setTokenServerRule(token, server, rule, options = {}) {
    const tokenID = Number(token.id);
    const serverID = Number(server.id);
    const savingKey = `${tokenID}:${serverID}`;
    const currentMap = permissionState.data[tokenID] || {};
    const currentPermission = normalizePermission(currentMap[serverID]);
    const nextMap = { ...currentMap };
    if (rule) {
      const expiresAt = Object.prototype.hasOwnProperty.call(options, "expiresAt")
        ? options.expiresAt || ""
        : currentPermission && !permissionExpired(currentPermission) && effectiveRule(currentPermission) === rule
          ? currentPermission.expires_at || ""
          : "";
      nextMap[serverID] = { execution_rule: rule, expires_at: expiresAt };
    } else {
      delete nextMap[serverID];
    }

    setPermissionState((current) => ({
      ...current,
      data: {
        ...current.data,
        [tokenID]: nextMap,
      },
      savingKey,
      error: null,
    }));

    try {
      const permissions = await apiPut(`/api/tokens/${tokenID}/permissions`, {
        permissions: permissionPayload(nextMap),
      });
      setPermissionState((current) => ({
        ...current,
        state: "ready",
        data: {
          ...current.data,
          [tokenID]: permissionsToMap(permissions),
        },
        savingKey: "",
        error: null,
      }));
    } catch (error) {
      setPermissionState((current) => ({
        ...current,
        data: {
          ...current.data,
          [tokenID]: currentMap,
        },
        state: "error",
        savingKey: "",
        error: error.message,
      }));
    }
  }

  async function setTokenAllServerRules(token, servers, rule, options = {}) {
    const tokenID = Number(token.id);
    const savingKey = `${tokenID}:all`;
    const currentMap = permissionState.data[tokenID] || {};
    const nextMap = {};
    if (rule) {
      const expiresAt = options.expiresAt || "";
      servers.forEach((server) => {
        nextMap[Number(server.id)] = { execution_rule: rule, expires_at: expiresAt };
      });
    }

    setPermissionState((current) => ({
      ...current,
      data: {
        ...current.data,
        [tokenID]: nextMap,
      },
      savingKey,
      error: null,
    }));

    try {
      const permissions = await apiPut(`/api/tokens/${tokenID}/permissions`, {
        permissions: permissionPayload(nextMap),
      });
      setPermissionState((current) => ({
        ...current,
        state: "ready",
        data: {
          ...current.data,
          [tokenID]: permissionsToMap(permissions),
        },
        savingKey: "",
        error: null,
      }));
    } catch (error) {
      setPermissionState((current) => ({
        ...current,
        data: {
          ...current.data,
          [tokenID]: currentMap,
        },
        state: "error",
        savingKey: "",
        error: error.message,
      }));
    }
  }

  return { permissionState, loadAllTokenPermissions, setTokenServerRule, setTokenAllServerRules };
}

function permissionPayload(permissionMap) {
  return Object.entries(permissionMap)
    .map(([id, permission]) => {
      const normalized = normalizePermission(permission);
      if (!normalized?.execution_rule) return null;
      if (permissionExpired(normalized)) return null;
      return {
        server_id: Number(id),
        execution_rule: normalized.execution_rule,
        expires_at: normalized.expires_at || "",
      };
    })
    .filter(Boolean);
}
