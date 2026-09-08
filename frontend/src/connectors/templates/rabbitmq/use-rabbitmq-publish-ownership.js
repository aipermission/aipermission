import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { apiGet } from "../../../lib/api";
import { connectorActionPending } from "../_shared/action-result";

const terminalPublishStatuses = new Set(["completed", "failed", "canceled", "blocked", "stale", "declined", "error"]);
const discoveryRetryMilliseconds = 3000;

export function useRabbitMQPublishOwnership(scopeKey, activeItems, approvalState, getAction = apiGet) {
  const ownerRef = useRef(null);
  const attemptSequenceRef = useRef(0);
  const discoveryGenerationRef = useRef(0);
  const [owner, setOwner] = useState(null);
  const [discovery, setDiscovery] = useState({ scopeKey: "", pending: true });
  const unresolved = useMemo(
    () =>
      activeItems.find(
        (item) => item.action_name === "publish_message" && (connectorActionPending(item) || item.status === "outcome_unknown"),
      ) || null,
    [activeItems],
  );
  const replace = useCallback((next) => {
    ownerRef.current = next;
    setOwner(next);
  }, []);
  const begin = useCallback(() => {
    const attemptID = ++attemptSequenceRef.current;
    replace({ attemptID, requestID: null, observed: false });
    return attemptID;
  }, [replace]);
  const update = useCallback(
    (attemptID, next) => {
      if (ownerRef.current?.attemptID !== attemptID) return false;
      replace({ attemptID, ...next });
      return true;
    },
    [replace],
  );
  const release = useCallback(
    (attemptID) => {
      if (attemptID !== undefined && ownerRef.current?.attemptID !== attemptID) return false;
      replace(null);
      return true;
    },
    [replace],
  );

  useEffect(() => {
    release();
  }, [release, scopeKey]);

  useEffect(() => {
    if (approvalState !== "ready") return;
    if (unresolved || ownerRef.current) {
      setDiscovery({ scopeKey, pending: false });
      return;
    }
    const generation = ++discoveryGenerationRef.current;
    let controller = null;
    let retryTimer = null;
    setDiscovery({ scopeKey, pending: true });
    const discover = () => {
      controller = new AbortController();
      const query = new URLSearchParams({ target_ref: scopeKey, action_name: "publish_message", active: "true" });
      void getAction(`/api/connector-action-approvals?${query.toString()}`, { signal: controller.signal })
        .then((items) => {
          if (generation !== discoveryGenerationRef.current) return;
          const active = Array.isArray(items) ? items[0] : null;
          if (active && !ownerRef.current) {
            const attemptID = ++attemptSequenceRef.current;
            replace({ attemptID, requestID: Number(active.id || active.request_id), observed: true });
          }
          setDiscovery({ scopeKey, pending: false });
        })
        .catch(() => {
          if (generation !== discoveryGenerationRef.current || controller.signal.aborted) return;
          retryTimer = window.setTimeout(discover, discoveryRetryMilliseconds);
        });
    };
    discover();
    return () => {
      controller?.abort();
      if (retryTimer !== null) window.clearTimeout(retryTimer);
    };
  }, [approvalState, getAction, replace, scopeKey, unresolved]);

  useEffect(() => {
    if (approvalState !== "ready") return;
    if (!owner) {
      if (unresolved) {
        const attemptID = ++attemptSequenceRef.current;
        replace({ attemptID, requestID: Number(unresolved.id || unresolved.request_id), observed: true });
      }
      return;
    }
    if (!owner.requestID) return;
    const action = activeItems.find((item) => Number(item.id || item.request_id) === owner.requestID);
    if (action && (connectorActionPending(action) || action.status === "outcome_unknown")) {
      if (!owner.observed) update(owner.attemptID, { requestID: owner.requestID, observed: true });
      return;
    }
    if (action && terminalPublishStatuses.has(action.status)) {
      release(owner.attemptID);
      return;
    }
    if (action) return;
    const controller = new AbortController();
    void getAction(`/api/connector-action-approvals/${owner.requestID}`, { signal: controller.signal })
      .then((exact) => {
        if (!terminalPublishStatuses.has(exact?.status)) {
          return;
        }
        release(owner.attemptID);
      })
      .catch(() => {});
    return () => controller.abort();
  }, [activeItems, approvalState, getAction, owner, release, replace, unresolved, update]);

  const discoveryPending = approvalState !== "ready" || discovery.scopeKey !== scopeKey || discovery.pending;
  return { ownerRef, locked: Boolean(owner || unresolved || discoveryPending), begin, update, release, unresolved };
}
