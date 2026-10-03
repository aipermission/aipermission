import { useLayoutEffect, useRef, useState } from "react";
import type { RefObject } from "react";
import { apiGet } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import { tokenProjectScopeSnapshot } from "../../lib/gateway-contracts/security-contracts";
import type { TokenProjectScope } from "../../lib/gateway-contracts/security-contracts";
import type { GatewayToken } from "../../lib/gateway-contracts/core-resource-contracts";
import { updateTokenProjectVisibility } from "../../lib/project-scopes";
import { useRequestGuard } from "../../lib/request-guard";

type Snapshot = {
  scope: string;
  items: Record<number, TokenProjectScope[]>;
  revisions: Record<number, string>;
  states: Record<number, "loading" | "ready" | "error" | "saving">;
  error: string;
};
type Options = {
  scope: string;
  projectID?: number;
  tokens: GatewayToken[];
  permissionMutationActiveRef: RefObject<boolean>;
  onSaved?: () => unknown | Promise<unknown>;
};

export function useConsoleProjectScopes({ scope, projectID, tokens, permissionMutationActiveRef, onSaved }: Options) {
  const requests = useRequestGuard(scope);
  const [snapshot, setSnapshot] = useState<Snapshot>({ scope, items: {}, revisions: {}, states: {}, error: "" });
  const [save, setSave] = useState({ scope, key: "" });
  const scopeRef = useRef(scope);
  const write = useRef<{ tokenID: number; request: ReturnType<typeof requests.begin> } | null>(null);
  const committed = useRef<{ load: typeof load; setVisibility: typeof setVisibility } | null>(null);

  useLayoutEffect(() => {
    requests.setScope(scope);
    if (scopeRef.current !== scope) {
      scopeRef.current = scope;
      setSnapshot({ scope, items: {}, revisions: {}, states: {}, error: "" });
      setSave({ scope, key: "" });
    }
    if (write.current && !write.current.request.isCurrent()) write.current = null;
  }, [requests, scope]);

  useLayoutEffect(() => {
    committed.current = { load, setVisibility };
    return () => {
      committed.current = null;
    };
  });

  const visible = snapshot.scope === scope;
  const isSaving = () => Boolean(write.current?.request.isCurrent());
  const ready = (tokenID: number) => visible && snapshot.states[tokenID] === "ready";

  async function load() {
    if (committed.current?.load !== load || !projectID) return;
    await Promise.all(
      tokens.map(async (token) => {
        if (write.current?.tokenID === token.id && isSaving()) return;
        const request = requests.begin(`read:${token.id}`);
        setSnapshot((current) => ({ ...current, scope, states: { ...current.states, [token.id]: "loading" }, error: "" }));
        try {
          const value = await apiGet(`/api/tokens/${token.id}/project-scopes`, { signal: request.signal });
          if (!request.isCurrent()) return;
          const result = tokenProjectScopeSnapshot(value);
          setSnapshot((current) => ({
            ...current,
            scope,
            items: { ...current.items, [token.id]: result.items },
            revisions: { ...current.revisions, [token.id]: result.revision },
            states: { ...current.states, [token.id]: "ready" },
          }));
        } catch (error) {
          if (!request.isCurrent()) return;
          setSnapshot((current) => ({
            ...current,
            scope,
            states: { ...current.states, [token.id]: "error" },
            error: errorMessage(error, "Failed to load token project scopes."),
          }));
        } finally {
          request.complete();
        }
      }),
    );
  }

  async function setVisibility(token: GatewayToken, enabled: boolean) {
    if (committed.current?.setVisibility !== setVisibility || !projectID || !ready(token.id)) return;
    if (isSaving() || permissionMutationActiveRef.current || !tokens.some((item) => item.id === token.id)) return;
    requests.invalidate(`read:${token.id}`);
    const request = requests.begin(`write:${token.id}`);
    const owner = { tokenID: token.id, request };
    write.current = owner;
    setSave({ scope, key: `${token.id}:project:${projectID}` });
    setSnapshot((current) => ({ ...current, states: { ...current.states, [token.id]: "saving" }, error: "" }));
    try {
      const result = await updateTokenProjectVisibility(token.id, snapshot.items[token.id] || [], projectID, enabled, {
        expectedRevision: snapshot.revisions[token.id],
        signal: request.signal,
      });
      if (!request.isCurrent()) return;
      setSnapshot((current) => ({
        ...current,
        items: { ...current.items, [token.id]: result.items },
        revisions: { ...current.revisions, [token.id]: result.revision },
        states: { ...current.states, [token.id]: "ready" },
      }));
      try {
        await onSaved?.();
      } catch (error) {
        if (request.isCurrent())
          setSnapshot((current) => ({
            ...current,
            error: `Project visibility saved, but refreshing connector permissions failed: ${errorMessage(error, "Unknown error.")}`,
          }));
      }
    } catch (error) {
      if (!request.isCurrent()) return;
      setSnapshot((current) => ({
        ...current,
        states: { ...current.states, [token.id]: "ready" },
        error: errorMessage(error, "Failed to update token project scope."),
      }));
    } finally {
      if (write.current === owner) {
        write.current = null;
        setSave({ scope, key: "" });
      }
      request.complete();
    }
  }

  return {
    load,
    isSaving,
    savingKey: save.scope === scope ? save.key : "",
    projectScopeError: visible ? snapshot.error : "",
    projectScopeReadyForToken: ready,
    projectEnabledForToken: (tokenID: number) =>
      visible && (snapshot.items[tokenID] || []).some((item) => item.project_id === projectID && item.enabled),
    setProjectVisibility: setVisibility,
  };
}
