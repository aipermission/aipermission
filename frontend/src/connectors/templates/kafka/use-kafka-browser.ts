import { useEffect, useEffectEvent, useMemo, useState } from "react";
import { useRequestGuard } from "../../../lib/request-guard";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { detailMatchesSelection } from "./console-helpers";
import { kafkaOutputDetail, kafkaOutputResources } from "./resource-output";
import type { KafkaBrowserProps, KafkaDetail, KafkaResource, KafkaView } from "./console-types";

const defaultRead = Object.freeze({ partition: "0", start_position: "recent", offset: "0", max_records: "20" });

export function useKafkaBrowser({ target, approvals, session, onRefreshActivity }: KafkaBrowserProps) {
  const activeSession = session || { active: false, startedAt: "" };
  const product = target.config?.server_family === "redpanda" ? "Redpanda" : "Kafka";
  const [view, setView] = useState<KafkaView>("topics");
  const [query, setQuery] = useState("");
  const [topics, setTopics] = useState<KafkaResource[]>([]);
  const [groups, setGroups] = useState<KafkaResource[]>([]);
  const [selectedName, setSelectedName] = useState("");
  const [detail, setDetail] = useState<KafkaDetail | null>(null);
  const [detailIdentity, setDetailIdentity] = useState("");
  const [messages, setMessages] = useState<unknown>(null);
  const [readForm, setReadForm] = useState<{ partition: string; start_position: string; offset: string; max_records: string }>(defaultRead);
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const scopeKey = `${target.ref}:${activeSession.startedAt || "inactive"}`;
  const requestGuard = useRequestGuard(scopeKey);
  const items = view === "topics" ? topics : groups;
  const filteredItems = useMemo(() => filterItems(items, query), [items, query]);
  const activeDetail = detailMatchesSelection(detailIdentity, view, selectedName) ? detail : null;
  const latestAction = useMemo(
    () => (approvals?.data || []).find((item) => item.target_ref === target.ref) || null,
    [approvals?.data, target.ref],
  );
  const refreshForEffect = useEffectEvent((nextView: KafkaView) => refreshList(nextView));

  useEffect(() => {
    setView("topics");
    setQuery("");
    setTopics([]);
    setGroups([]);
    requestGuard.invalidate("messages");
    setSelectedName("");
    setDetail(null);
    setDetailIdentity("");
    setMessages(null);
    setReadForm(defaultRead);
    setState({ state: "idle", error: "", message: "" });
  }, [requestGuard, scopeKey]);

  useEffect(() => {
    if (activeSession.active) void refreshForEffect("topics");
  }, [activeSession.active, activeSession.startedAt, target.ref]);

  async function runAction(actionName: string, input: Record<string, unknown>, reason: string, busy = "loading", channel = actionName) {
    try {
      const item = await runGuardedConnectorAction({
        requestGuard,
        channel,
        targetRef: target.ref,
        actionName,
        input,
        reason,
        busy,
        product,
        setState,
        onRefreshActivity,
      });
      return item?.output || null;
    } catch {
      return null;
    }
  }

  async function refreshList(nextView = view) {
    if (!activeSession.active) return;
    const topicMode = nextView === "topics";
    const output = await runAction(
      topicMode ? "list_topics" : "list_consumer_groups",
      topicMode ? { include_internal: false } : {},
      `manual ${product} browser ${topicMode ? "topic" : "consumer group"} list`,
      "loading",
      `list:${nextView}`,
    );
    if (!output) return;
    if (topicMode) setTopics(kafkaOutputResources(output, "topics"));
    else setGroups(kafkaOutputResources(output, "consumer_groups"));
  }

  async function changeView(nextView: KafkaView) {
    if (nextView === view) return;
    setView(nextView);
    clearSelection();
    if ((nextView === "topics" ? topics : groups).length === 0) await refreshList(nextView);
  }

  async function selectItem(item: KafkaResource) {
    if (selectedName === item.name) {
      clearSelection();
      return;
    }
    requestGuard.invalidate("messages");
    setSelectedName(item.name);
    setDetail(null);
    setDetailIdentity("");
    setMessages(null);
    await loadDetail(item.name, view);
  }

  async function loadDetail(name: string, detailView = view) {
    setDetail(null);
    setDetailIdentity("");
    const output = await runAction(
      detailView === "topics" ? "describe_topic" : "describe_consumer_group",
      detailView === "topics" ? { topic: name } : { group: name },
      `manual ${product} browser ${detailView === "topics" ? "topic" : "consumer group"} detail`,
      "reading",
      "detail",
    );
    const next = kafkaOutputDetail(output);
    if (!next) return null;
    setDetail(next);
    setDetailIdentity(`${detailView}:${name}`);
    if (detailView === "topics" && next.partitions?.length) {
      setReadForm((current) => ({ ...current, partition: String(next.partitions?.[0].partition) }));
    }
    return next;
  }

  async function readMessages() {
    if (!selectedName) return;
    const input: Record<string, unknown> = {
      topic: selectedName,
      partition: Number(readForm.partition),
      start_position: readForm.start_position,
      max_records: Number(readForm.max_records),
      max_bytes: 262144,
      wait_seconds: 2,
    };
    if (readForm.start_position === "offset") input.offset = readForm.offset;
    const output = await runAction("read_messages", input, `manual ${product} browser bounded message sample`, "reading", "messages");
    if (output) setMessages(output);
  }

  function clearSelection() {
    requestGuard.invalidate("detail");
    requestGuard.invalidate("messages");
    setState((current) => (current.state === "reading" ? { state: "idle", error: "", message: "" } : current));
    setSelectedName("");
    setDetail(null);
    setDetailIdentity("");
    setMessages(null);
  }

  return {
    activeSession,
    product,
    scopeKey,
    view,
    query,
    setQuery,
    filteredItems,
    selectedName,
    activeDetail,
    messages,
    readForm,
    setReadForm,
    state,
    setState,
    latestAction,
    refreshList,
    changeView,
    selectItem,
    loadDetail,
    readMessages,
    runAction,
  };
}

function filterItems(items: KafkaResource[], query: string) {
  const needle = query.trim().toLowerCase();
  if (!needle) return items;
  return items.filter((item) =>
    String(item.name || "")
      .toLowerCase()
      .includes(needle),
  );
}
