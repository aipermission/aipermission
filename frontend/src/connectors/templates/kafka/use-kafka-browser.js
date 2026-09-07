import { useEffect, useEffectEvent, useMemo, useState } from "react";
import { useRequestGuard } from "../../../lib/request-guard";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { detailMatchesSelection } from "./console-helpers";

const defaultRead = Object.freeze({ partition: "0", start_position: "recent", offset: "0", max_records: "20" });

export function useKafkaBrowser({ target, approvals, session, onRefreshActivity }) {
  const activeSession = session || { active: false, startedAt: "" };
  const product = target.config?.server_family === "redpanda" ? "Redpanda" : "Kafka";
  const [view, setView] = useState("topics");
  const [query, setQuery] = useState("");
  const [topics, setTopics] = useState([]);
  const [groups, setGroups] = useState([]);
  const [selectedName, setSelectedName] = useState("");
  const [detail, setDetail] = useState(null);
  const [detailIdentity, setDetailIdentity] = useState("");
  const [messages, setMessages] = useState(null);
  const [readForm, setReadForm] = useState(defaultRead);
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
  const refreshForEffect = useEffectEvent((nextView) => refreshList(nextView));

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

  async function runAction(actionName, input, reason, busy = "loading", channel = actionName) {
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
    if (topicMode) setTopics(Array.isArray(output.topics) ? output.topics : []);
    else setGroups(Array.isArray(output.consumer_groups) ? output.consumer_groups : []);
  }

  async function changeView(nextView) {
    if (nextView === view) return;
    setView(nextView);
    clearSelection();
    if ((nextView === "topics" ? topics : groups).length === 0) await refreshList(nextView);
  }

  async function selectItem(item) {
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

  async function loadDetail(name, detailView = view) {
    setDetail(null);
    setDetailIdentity("");
    const output = await runAction(
      detailView === "topics" ? "describe_topic" : "describe_consumer_group",
      detailView === "topics" ? { topic: name } : { group: name },
      `manual ${product} browser ${detailView === "topics" ? "topic" : "consumer group"} detail`,
      "reading",
      "detail",
    );
    if (!output) return null;
    setDetail(output);
    setDetailIdentity(`${detailView}:${name}`);
    if (detailView === "topics" && Array.isArray(output.partitions) && output.partitions.length > 0) {
      setReadForm((current) => ({ ...current, partition: String(output.partitions[0].partition) }));
    }
    return output;
  }

  async function readMessages() {
    if (!selectedName) return;
    const input = {
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
    requestGuard.invalidate("messages");
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

function filterItems(items, query) {
  const needle = query.trim().toLowerCase();
  if (!needle) return items;
  return items.filter((item) =>
    String(item.name || "")
      .toLowerCase()
      .includes(needle),
  );
}
