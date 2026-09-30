import { useCallback, useEffect, useRef, useState } from "react";
import { apiGet, currentWorkspaceBinding } from "../../../lib/api";
import { definitiveMutationFailure, observeMutationRequests } from "./mutation-observation";
import { connectorActionUnknownOutcome } from "./action-runner";
import { scheduleObservation } from "./observation-scheduler";
import type { ObservationScheduler } from "./observation-scheduler";
import type { MutationLookup } from "./mutation-observation";
import type { ConnectorActionResponse, ConnectorApproval } from "../../../lib/gateway-contracts/security-contracts";

type MutationOwner = { attemptID: number; requestID: number | null; observed: boolean; scope: string; inFlight: boolean };
const observationInterval = 3000;
export type MutationObservationScheduler = ObservationScheduler;

export function useConnectorMutationOwnership(
  targetRef: string,
  actionNames: readonly string[],
  approvalState?: string,
  getAction: MutationLookup = apiGet,
  schedule: MutationObservationScheduler = scheduleObservation,
) {
  const workspaceID = currentWorkspaceBinding();
  const scope = `${workspaceID}:${targetRef}`;
  const actionKey = actionNames.join(",");
  const ownerRef = useRef<MutationOwner | null>(null);
  const attemptSequence = useRef(0);
  const [owner, setOwner] = useState<MutationOwner | null>(null);
  const [discovery, setDiscovery] = useState({ scope: "", pending: true, unresolved: null as ConnectorApproval | null });
  const discoveryRef = useRef(discovery);
  const replace = useCallback((next: MutationOwner | null) => {
    ownerRef.current = next;
    setOwner(next);
  }, []);
  const begin = useCallback(() => {
    if (
      approvalState !== "ready" ||
      ownerRef.current?.scope === scope ||
      discoveryRef.current.scope !== scope ||
      discoveryRef.current.pending ||
      discoveryRef.current.unresolved
    )
      return 0;
    const attemptID = ++attemptSequence.current;
    replace({ attemptID, requestID: null, observed: false, scope, inFlight: true });
    return attemptID;
  }, [approvalState, replace, scope]);
  const update = useCallback(
    (attemptID: number, next: Pick<MutationOwner, "requestID" | "observed">) => {
      if (ownerRef.current?.attemptID !== attemptID || ownerRef.current.scope !== scope) return false;
      replace({ attemptID, ...next, scope, inFlight: false });
      return true;
    },
    [replace, scope],
  );

  const release = useCallback(
    (attemptID?: number) => {
      if (ownerRef.current?.scope !== scope || (attemptID !== undefined && ownerRef.current.attemptID !== attemptID)) return false;
      replace(null);
      return true;
    },
    [replace, scope],
  );
  const run = useCallback(
    async (operation: (_onPending: (_item: ConnectorActionResponse) => void) => Promise<ConnectorActionResponse | null>) => {
      const attemptID = begin();
      if (!attemptID) return null;
      try {
        const item = await operation((pending) => update(attemptID, { requestID: pending.request_id, observed: true }));
        if (item && !release(attemptID)) return null;
        return item;
      } catch (error) {
        const uncertain = connectorActionUnknownOutcome(error);
        if (typeof uncertain?.request_id === "number" && Number.isSafeInteger(uncertain.request_id) && uncertain.request_id > 0)
          update(attemptID, { requestID: uncertain.request_id, observed: true });
        else if (definitiveMutationFailure(error, targetRef, actionNames)) release(attemptID);
        throw error;
      } finally {
        const current = ownerRef.current;
        if (current?.attemptID === attemptID && current.scope === scope) replace({ ...current, inFlight: false });
      }
    },
    [actionNames, begin, release, replace, scope, targetRef, update],
  );

  useEffect(() => {
    if (ownerRef.current?.scope !== scope) replace(null);
    let active = true;
    let cancelTimer: (() => void) | undefined;
    let controller: AbortController | undefined;
    const names = actionKey.split(",");
    const setDiscoveryState = (pending: boolean, unresolved: ConnectorApproval | null = null) => {
      const next = { scope, pending, unresolved };
      discoveryRef.current = next;
      setDiscovery(next);
    };
    setDiscoveryState(true);
    const observe = async () => {
      controller = new AbortController();
      try {
        const current = ownerRef.current;
        const { unresolved, retained, terminalIDs, reconciledIDs } = await observeMutationRequests({
          targetRef,
          actions: names,
          workspaceID,
          requestID: current?.scope === scope ? current.requestID : null,
          get: getAction,
          signal: controller.signal,
        });
        if (!active) return;
        const settled = current?.requestID ? terminalIDs.has(current.requestID) || reconciledIDs.has(current.requestID) : false;
        if (!retained && !unresolved && !current?.inFlight && ownerRef.current === current && (settled || (current && !current.requestID)))
          replace(null);
        if (unresolved && !ownerRef.current)
          replace({ attemptID: ++attemptSequence.current, requestID: unresolved.id, observed: true, scope, inFlight: false });
        else if (unresolved && current && !current.requestID && ownerRef.current === current)
          replace({ ...current, requestID: unresolved.id, observed: true });
        setDiscoveryState(retained, unresolved);
      } catch {
        if (active) setDiscoveryState(true);
      } finally {
        if (active) cancelTimer = schedule(observe, observationInterval);
      }
    };
    if (approvalState === "ready") void observe();
    return () => {
      active = false;
      cancelTimer?.();
      controller?.abort();
    };
  }, [actionKey, approvalState, getAction, replace, schedule, scope, targetRef, workspaceID]);

  return {
    ownerRef,
    begin,
    update,
    release,
    run,
    unresolved: discovery.unresolved,
    locked:
      approvalState !== "ready" ||
      discovery.scope !== scope ||
      discovery.pending ||
      Boolean(discovery.unresolved || owner?.scope === scope),
  };
}
