import { useEffect, useEffectEvent, useMemo, useState } from "react";
import { useRequestGuard } from "../../../lib/request-guard";
import { connectorActionBusy } from "../_shared/action-state";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { connectorActionRequestID } from "../_shared/action-result";
import { filterQueues, isRabbitRecord, parsePublishProperties, readRabbitQueue, readRabbitQueues, readRabbitRecords } from "./helpers";
import { useRabbitMQPublishOwnership } from "./use-rabbitmq-publish-ownership";
import type { RabbitActivity, RabbitBrowserProps, RabbitMessage, RabbitQueue } from "./browser-types";

type RabbitActionOptions = {
  actionName: string;
  input: Record<string, unknown>;
  reason: string;
  busy?: string;
  suppressError?: boolean;
  channel?: string;
  onPending?: (_item: RabbitActivity) => void;
};

const defaultQueueLimit = 250;
const defaultPeekCount = 5;
const defaultPayloadBytes = 65536;
const defaultProperties = '{"content_type":"application/json"}';

export function useRabbitMQBrowser({ target, approvals, session, onRefreshActivity }: RabbitBrowserProps) {
  const activeSession = session || { active: false, startedAt: "" };
  const [pattern, setPattern] = useState("");
  const [vhost, setVhost] = useState(target.config?.vhost || "/");
  const [vhostDraft, setVhostDraft] = useState(target.config?.vhost || "/");
  const [queues, setQueues] = useState<RabbitQueue[]>([]);
  const [activeQueue, setActiveQueue] = useState("");
  const [queueDetail, setQueueDetail] = useState<RabbitQueue | null>(null);
  const [bindings, setBindings] = useState<Record<string, unknown>[]>([]);
  const [messages, setMessages] = useState<RabbitMessage[]>([]);
  const [peekCount, setPeekCount] = useState<string | number>(defaultPeekCount);
  const [detailMode, setDetailMode] = useState<"inspect" | "publish">("inspect");
  const [publish, setPublish] = useState({
    exchange: "amq.default",
    customRoutingKey: false,
    routingKey: "",
    payload: "",
    properties: defaultProperties,
  });
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const sessionScopeKey = `${target.ref}:${activeSession.startedAt || "inactive"}`;
  const requestScopeKey = `${sessionScopeKey}:${vhost}`;
  const requestGuard = useRequestGuard(requestScopeKey);
  const filteredQueues = useMemo(() => filterQueues(queues, pattern), [queues, pattern]);
  const activeItems = useMemo(
    () => (approvals?.data || []).filter((item) => item.target_ref === target.ref),
    [approvals?.data, target.ref],
  );
  const publishOwnership = useRabbitMQPublishOwnership(target.ref, activeItems, approvals?.state);
  const publishOwnerRef = publishOwnership.ownerRef;
  const unresolvedPublish = publishOwnership.unresolved;
  const beginPublishOwner = publishOwnership.begin;
  const updatePublishOwner = publishOwnership.update;
  const releasePublishOwner = publishOwnership.release;
  const refreshForEffect = useEffectEvent(() => refreshQueues());

  useEffect(() => {
    setVhost(target.config?.vhost || "/");
    setVhostDraft(target.config?.vhost || "/");
    setQueues([]);
    setActiveQueue("");
    setQueueDetail(null);
    setBindings([]);
    setMessages([]);
    setPattern("");
    setPeekCount(defaultPeekCount);
    setDetailMode("inspect");
    setPublish({ exchange: "amq.default", customRoutingKey: false, routingKey: "", payload: "", properties: defaultProperties });
    setState({ state: "idle", error: "", message: "" });
  }, [sessionScopeKey, target.config?.vhost]);

  useEffect(() => {
    if (activeSession.active) void refreshForEffect();
  }, [activeSession.active, activeSession.startedAt, target.ref, vhost]);

  useEffect(() => {
    if (activeQueue) return;
    for (const channel of ["queue-selection", "get_queue", "list_bindings", "peek_messages"]) requestGuard.invalidate(channel);
    setQueueDetail(null);
    setBindings([]);
    setMessages([]);
  }, [activeQueue, requestGuard]);

  async function runRabbitAction({
    actionName,
    input,
    reason,
    busy = "running",
    suppressError = false,
    channel = actionName,
    onPending,
  }: RabbitActionOptions) {
    return runGuardedConnectorAction({
      requestGuard,
      channel,
      targetRef: target.ref,
      actionName,
      input,
      reason,
      busy,
      product: "RabbitMQ",
      setState,
      onRefreshActivity,
      suppressError,
      onPending,
    });
  }

  async function refreshQueues() {
    if (!activeSession.active || publishOwnerRef.current || unresolvedPublish) return;
    try {
      const item = await runRabbitAction({
        actionName: "list_queues",
        input: { vhost, pattern: "", limit: defaultQueueLimit },
        reason: "manual RabbitMQ browser queue list",
        busy: "loading",
      });
      if (!item) return;
      const next = readRabbitQueues(isRabbitRecord(item.output) ? item.output.queues : null);
      setQueues(next);
      setActiveQueue((current) => (current && !next.some((queue) => queue.name === current) ? "" : current));
    } catch {
      // The guarded runner owns the visible error state.
    }
  }

  async function selectQueue(queueName: string) {
    if (!activeSession.active || !queueName || publishOwnerRef.current || unresolvedPublish) return;
    const selection = requestGuard.begin("queue-selection");
    requestGuard.invalidate("list_bindings");
    requestGuard.invalidate("peek_messages");
    setActiveQueue(queueName);
    setQueueDetail(null);
    setBindings([]);
    setDetailMode("inspect");
    setPublish((current) => ({ ...current, customRoutingKey: false, routingKey: queueName }));
    setMessages([]);
    try {
      const detail = await runRabbitAction({
        actionName: "get_queue",
        input: { vhost, queue: queueName },
        reason: "manual RabbitMQ browser queue detail",
        busy: "reading",
      });
      if (!detail || !selection.isCurrent()) return;
      setQueueDetail(readRabbitQueue(detail.output));
      const binding = await runRabbitAction({
        actionName: "list_bindings",
        input: { vhost, queue: queueName, limit: 250 },
        reason: "manual RabbitMQ browser queue bindings",
        busy: "reading",
        suppressError: true,
      });
      if (binding && selection.isCurrent()) setBindings(readRabbitRecords(isRabbitRecord(binding.output) ? binding.output.bindings : null));
    } catch {
      if (selection.isCurrent()) setBindings([]);
    } finally {
      selection.complete();
    }
  }

  async function peekMessages() {
    if (!activeQueue || publishOwnerRef.current || unresolvedPublish) return;
    try {
      const item = await runRabbitAction({
        actionName: "peek_messages",
        input: { vhost, queue: activeQueue, count: Number(peekCount) || defaultPeekCount, max_payload_bytes: defaultPayloadBytes },
        reason: "manual RabbitMQ browser message peek",
        busy: "peeking",
      });
      if (item) setMessages(readRabbitRecords(isRabbitRecord(item.output) ? item.output.messages : null));
    } catch {
      // The guarded runner owns the visible error state.
    }
  }

  function startPublish() {
    if (publishOwnerRef.current || unresolvedPublish) return;
    setDetailMode("publish");
    setState({ state: "idle", error: "", message: "" });
    setPublish((current) =>
      !current.routingKey.trim() && activeQueue ? { ...current, customRoutingKey: false, routingKey: activeQueue } : current,
    );
  }

  function applyVhost() {
    if (publishOwnerRef.current || unresolvedPublish) return;
    const nextVhost = vhostDraft.trim() || "/";
    if (nextVhost === vhost) {
      void refreshQueues();
      return;
    }
    requestGuard.setScope(`${sessionScopeKey}:${nextVhost}`);
    setVhost(nextVhost);
    setVhostDraft(nextVhost);
    setActiveQueue("");
    setQueueDetail(null);
    setBindings([]);
    setMessages([]);
    setState({ state: "idle", error: "", message: "" });
  }

  async function publishMessage() {
    if (!activeSession.active || connectorActionBusy(state) || publishOwnership.locked) return;
    const routingKey = publish.routingKey.trim();
    if (!routingKey || !publish.payload) {
      setState({ state: "error", error: "Routing key and payload are required.", message: "" });
      return;
    }
    const properties = parsePublishProperties(publish.properties);
    if (properties.value === null) {
      setState({ state: "error", error: properties.error, message: "" });
      return;
    }
    const attemptID = beginPublishOwner();
    let pendingRequestID: number | null = null;
    try {
      const item = await runRabbitAction({
        actionName: "publish_message",
        input: {
          vhost,
          exchange: publish.exchange.trim() || "amq.default",
          routing_key: routingKey,
          payload: publish.payload,
          payload_encoding: "string",
          properties: properties.value,
        },
        reason: "manual RabbitMQ browser message publish",
        busy: "publishing",
        onPending: (pending) => {
          pendingRequestID = connectorActionRequestID(pending);
          updatePublishOwner(attemptID, { requestID: pendingRequestID, observed: false });
        },
      });
      if (!item) {
        if (!pendingRequestID) releasePublishOwner(attemptID);
        return;
      }
      if (!releasePublishOwner(attemptID)) return;
      setPublish((current) => ({ ...current, payload: "" }));
      await refreshQueues();
    } catch (error) {
      const uncertain = unknownPublishOutcome(error);
      if (uncertain) {
        updatePublishOwner(attemptID, { requestID: uncertain.requestID, observed: false });
      } else {
        releasePublishOwner(attemptID);
      }
      // The guarded runner owns the visible error state.
    }
  }

  return {
    activeSession,
    pattern,
    setPattern,
    vhost,
    vhostDraft,
    setVhostDraft,
    applyVhost,
    queues,
    filteredQueues,
    activeQueue,
    queueDetail,
    bindings,
    messages,
    peekCount,
    setPeekCount,
    detailMode,
    setDetailMode,
    publish,
    setPublish,
    state,
    publishLocked: publishOwnership.locked,
    latestAction: activeItems.at(0) || null,
    refreshQueues,
    selectQueue,
    peekMessages,
    startPublish,
    publishMessage,
  };
}

export type RabbitBrowser = ReturnType<typeof useRabbitMQBrowser>;

function unknownPublishOutcome(error: unknown): { requestID: number | null } | null {
  const actionItem: unknown = isRabbitRecord(error) ? error.actionItem : null;
  const uncertain: unknown =
    isRabbitRecord(actionItem) && actionItem.status === "outcome_unknown" ? actionItem : isRabbitRecord(error) ? error.data : null;
  if (!isRabbitRecord(uncertain) || uncertain.status !== "outcome_unknown") return null;
  const requestID = Number(uncertain.request_id);
  return { requestID: Number.isInteger(requestID) && requestID > 0 ? requestID : null };
}
