import { useEffect, useEffectEvent, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useRequestGuard } from "../../../lib/request-guard";
import { currentWorkspaceBinding } from "../../../lib/api";
import { connectorActionBusy } from "../_shared/action-state";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { filterQueues, isRabbitRecord, parsePublishProperties, readRabbitQueue, readRabbitQueues, readRabbitRecords } from "./helpers";
import { useConnectorMutationOwnership } from "../_shared/use-connector-mutation-ownership";
import type { RabbitBrowserProps, RabbitMessage, RabbitQueue } from "./browser-types";
import type { ConnectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";

type RabbitActionOptions = {
  actionName: string;
  input: Record<string, unknown>;
  reason: string;
  busy?: string;
  suppressError?: boolean;
  channel?: string;
  onPending?: (_item: ConnectorActionResponse) => void;
};

const defaultQueueLimit = 250;
const defaultPeekCount = 5;
const defaultPayloadBytes = 65536;
const defaultProperties = '{"content_type":"application/json"}';
const mutationActions = ["publish_message"] as const;

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
  const sessionScopeKey = `${currentWorkspaceBinding()}:${target.ref}:${activeSession.startedAt || "inactive"}`;
  const requestScopeKey = JSON.stringify([sessionScopeKey, vhost, activeSession.active]);
  const requestGuard = useRequestGuard(requestScopeKey);
  const peekOwner = useMemo(() => ({ requestScopeKey, activeQueue }), [requestScopeKey, activeQueue]);
  const peekOwnerRef = useRef<object | null>(null);
  useLayoutEffect(() => {
    peekOwnerRef.current = peekOwner;
    return () => {
      peekOwnerRef.current = null;
      requestGuard.invalidate("peek_messages");
    };
  }, [peekOwner, requestGuard]);
  const filteredQueues = useMemo(() => filterQueues(queues, pattern), [queues, pattern]);
  const activeItems = useMemo(
    () => (approvals?.data || []).filter((item) => item.target_ref === target.ref),
    [approvals?.data, target.ref],
  );
  const publishOwnership = useConnectorMutationOwnership(target.ref, mutationActions, approvals?.state);
  const publishOwnerRef = publishOwnership.ownerRef;
  const unresolvedPublish = publishOwnership.unresolved;
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
      exclusiveMutationActions: actionName === "publish_message" ? mutationActions : undefined,
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
    if (!activeSession.active || !activeQueue || peekOwnerRef.current !== peekOwner || publishOwnerRef.current || unresolvedPublish) return;
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
      current.routingKey === "" && activeQueue ? { ...current, customRoutingKey: false, routingKey: activeQueue } : current,
    );
  }

  function applyVhost() {
    if (publishOwnerRef.current || unresolvedPublish) return;
    const nextVhost = vhostDraft || "/";
    if (nextVhost === vhost) {
      void refreshQueues();
      return;
    }
    requestGuard.setScope(JSON.stringify([sessionScopeKey, nextVhost, activeSession.active]));
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
    const routingKey = publish.routingKey;
    if (!routingKey || !publish.payload) {
      setState({ state: "error", error: "Routing key and payload are required.", message: "" });
      return;
    }
    const properties = parsePublishProperties(publish.properties);
    if (properties.value === null) {
      setState({ state: "error", error: properties.error, message: "" });
      return;
    }
    try {
      const item = await publishOwnership.run((onPending) =>
        runRabbitAction({
          actionName: "publish_message",
          input: {
            vhost,
            exchange: publish.exchange || "amq.default",
            routing_key: routingKey,
            payload: publish.payload,
            payload_encoding: "string",
            properties: properties.value,
          },
          reason: "manual RabbitMQ browser message publish",
          busy: "publishing",
          onPending,
        }),
      );
      if (!item) return;
      setPublish((current) => ({ ...current, payload: "" }));
      await refreshQueues();
    } catch {
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
