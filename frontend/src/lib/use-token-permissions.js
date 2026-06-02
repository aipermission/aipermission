import { useState } from "react";
import { apiGet, apiPut } from "./api";
import { permissionsToMap } from "./permissions";

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

  async function setTokenServerRule(token, server, rule) {
    const tokenID = Number(token.id);
    const serverID = Number(server.id);
    const savingKey = `${tokenID}:${serverID}`;
    const currentMap = permissionState.data[tokenID] || {};
    const nextMap = { ...currentMap };
    if (rule) {
      nextMap[serverID] = rule;
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
        permissions: Object.entries(nextMap).map(([id, executionRule]) => ({
          server_id: Number(id),
          execution_rule: executionRule,
        })),
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

  async function setTokenAllServerRules(token, servers, rule) {
    const tokenID = Number(token.id);
    const savingKey = `${tokenID}:all`;
    const currentMap = permissionState.data[tokenID] || {};
    const nextMap = {};
    if (rule) {
      servers.forEach((server) => {
        nextMap[Number(server.id)] = rule;
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
        permissions: Object.entries(nextMap).map(([id, executionRule]) => ({
          server_id: Number(id),
          execution_rule: executionRule,
        })),
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
