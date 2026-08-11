import { Activity, Database, Eye, Gauge, RefreshCcw, Search, Send, Users } from "lucide-react";
import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { apiPost } from "../../../lib/api";
import { connectorActionError, connectorActionPending } from "../_shared/action-result";
import {
  actionableOffsetPartitions,
  detailMatchesSelection,
  offsetSelectionValue,
  parseOffsetSelection,
  requestIsCurrent,
} from "./console-helpers";
import { KafkaOffsetDialog, KafkaPublishDialog } from "./write-dialogs";

const defaultRead = Object.freeze({ partition: "0", start_position: "recent", offset: "0", max_records: "20" });
const defaultPublish = Object.freeze({ partition: "0", key: "", key_encoding: "utf8", value: "", value_encoding: "utf8", headers: "[]" });
const defaultOffset = Object.freeze({ selection: "", offset: "" });

export function KafkaConnectorConsoleTemplate({ target, approvals, theme, session, onNewStructuredSession, onRefreshActivity }) {
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
  const [publishDialog, setPublishDialog] = useState({ open: false, form: defaultPublish, error: "" });
  const [offsetDialog, setOffsetDialog] = useState({ open: false, form: defaultOffset, error: "" });
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const requestVersions = useRef(new Map());
  const currentTargetRef = useRef(target.ref);
  const panelClass = theme === "light" ? "bg-white text-stone-900" : "bg-[#1e1e1e] text-stone-100";
  const mutedClass = theme === "light" ? "text-stone-500" : "text-stone-400";
  const borderClass = theme === "light" ? "border-stone-200" : "border-stone-700";
  const subtlePanelClass = theme === "light" ? "bg-stone-50" : "bg-[#252526]";
  const inputClass = theme === "light" ? "border-stone-300 bg-white text-stone-900" : "border-stone-700 bg-[#1a1a1a] text-stone-100";
  const hoverClass = theme === "light" ? "hover:bg-stone-50" : "hover:bg-stone-800/60";
  const activeClass =
    theme === "light" ? "border-emerald-200 bg-emerald-50 text-emerald-950" : "border-emerald-700 bg-emerald-950/40 text-emerald-100";
  const latestAction = useMemo(
    () => (approvals?.data || []).find((item) => item.target_ref === target.ref) || null,
    [approvals?.data, target.ref],
  );
  const items = view === "topics" ? topics : groups;
  const filteredItems = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return items;
    return items.filter((item) =>
      String(item.name || "")
        .toLowerCase()
        .includes(needle),
    );
  }, [items, query]);
  const activeDetail = detailMatchesSelection(detailIdentity, view, selectedName) ? detail : null;
  const offsetPartitions = actionableOffsetPartitions(activeDetail?.partitions);
  const refreshListForEffect = useEffectEvent((nextView) => refreshList(nextView));

  useEffect(() => {
    currentTargetRef.current = target.ref;
    requestVersions.current.clear();
    setView("topics");
    setQuery("");
    setTopics([]);
    setGroups([]);
    setSelectedName("");
    setDetail(null);
    setDetailIdentity("");
    setMessages(null);
    setReadForm(defaultRead);
    setPublishDialog({ open: false, form: defaultPublish, error: "" });
    setOffsetDialog({ open: false, form: defaultOffset, error: "" });
    setState({ state: "idle", error: "", message: "" });
  }, [target.ref, activeSession.startedAt]);

  useEffect(() => {
    if (!activeSession.active) return;
    void refreshListForEffect("topics");
  }, [activeSession.active, activeSession.startedAt, target.ref]);

  async function runAction(actionName, input, reason, busy = "loading", channel = actionName) {
    const version = (requestVersions.current.get(channel) || 0) + 1;
    const requestTargetRef = target.ref;
    requestVersions.current.set(channel, version);
    setState({ state: busy, error: "", message: "" });
    try {
      const item = await apiPost("/api/connector-actions/local-run", {
        target_ref: target.ref,
        action_name: actionName,
        input,
        reason,
      });
      if (!requestIsCurrent(requestVersions.current, channel, version, requestTargetRef, currentTargetRef.current)) return null;
      const actionError = connectorActionError(item, `${product} action failed.`);
      if (actionError) throw new Error(actionError);
      const message = item.display_text || "";
      const pending = connectorActionPending(item);
      try {
        await onRefreshActivity?.();
      } catch (refreshError) {
        if (!requestIsCurrent(requestVersions.current, channel, version, requestTargetRef, currentTargetRef.current)) return null;
        setState({
          state: "idle",
          error: `Action ${pending ? "is pending" : "completed"}, but activity refresh failed: ${refreshError.message || "unknown error"}`,
          message,
        });
        return pending ? null : item.output || {};
      }
      if (!requestIsCurrent(requestVersions.current, channel, version, requestTargetRef, currentTargetRef.current)) return null;
      setState({ state: "idle", error: "", message: message || (pending ? `${product} action is awaiting approval.` : "") });
      return pending ? null : item.output || {};
    } catch (error) {
      if (!requestIsCurrent(requestVersions.current, channel, version, requestTargetRef, currentTargetRef.current)) return null;
      setState({ state: "idle", error: error.message || `${product} action failed.`, message: "" });
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
    setView(nextView);
    setSelectedName("");
    setDetail(null);
    setDetailIdentity("");
    setMessages(null);
    if ((nextView === "topics" ? topics : groups).length === 0) await refreshList(nextView);
  }

  async function selectItem(item) {
    if (selectedName === item.name) {
      setSelectedName("");
      setDetail(null);
      setDetailIdentity("");
      setMessages(null);
      return;
    }
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
    if (output) {
      setDetail(output);
      setDetailIdentity(`${detailView}:${name}`);
      if (detailView === "topics" && Array.isArray(output.partitions) && output.partitions.length > 0) {
        setReadForm((current) => ({ ...current, partition: String(output.partitions[0].partition) }));
      }
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

  function openPublishDialog() {
    const firstPartition = activeDetail?.partitions?.[0]?.partition ?? 0;
    setState({ state: "idle", error: "", message: "" });
    setPublishDialog({ open: true, form: { ...defaultPublish, partition: String(firstPartition) }, error: "" });
  }

  async function publishMessage() {
    const form = publishDialog.form;
    let headers;
    try {
      headers = JSON.parse(form.headers || "[]");
    } catch {
      setPublishDialog((current) => ({ ...current, error: "Headers must be valid JSON." }));
      return;
    }
    if (!Array.isArray(headers)) {
      setPublishDialog((current) => ({ ...current, error: "Headers must be a JSON array." }));
      return;
    }
    setPublishDialog((current) => ({ ...current, error: "" }));
    const output = await runAction(
      "publish_message",
      {
        topic: selectedName,
        partition: Number(form.partition),
        key: form.key,
        key_encoding: form.key_encoding,
        value: form.value,
        value_encoding: form.value_encoding,
        headers,
      },
      `manual ${product} browser message publish`,
      "writing",
      "publish",
    );
    if (!output) return;
    setPublishDialog({ open: false, form: defaultPublish, error: "" });
    await loadDetail(selectedName, "topics");
  }

  function openOffsetDialog() {
    const first = offsetPartitions[0];
    const selection = first ? offsetSelectionValue(first) : "";
    const offset = first?.committed_offset === "-1" ? "" : String(first?.committed_offset || "");
    setState({ state: "idle", error: "", message: "" });
    setOffsetDialog({ open: true, form: { selection, offset }, error: "" });
  }

  async function setConsumerGroupOffset() {
    const selected = parseOffsetSelection(offsetDialog.form.selection);
    if (!selected) {
      setOffsetDialog((current) => ({ ...current, error: "Choose one topic partition." }));
      return;
    }
    if (!/^\d+$/.test(offsetDialog.form.offset.trim())) {
      setOffsetDialog((current) => ({ ...current, error: "New offset must be a non-negative integer." }));
      return;
    }
    setOffsetDialog((current) => ({ ...current, error: "" }));
    const output = await runAction(
      "set_consumer_group_offset",
      {
        group: selectedName,
        topic: selected.topic,
        partition: selected.partition,
        offset: offsetDialog.form.offset.trim(),
      },
      `manual ${product} browser consumer group offset change`,
      "writing",
      "offset",
    );
    if (!output) return;
    setOffsetDialog({ open: false, form: defaultOffset, error: "" });
    await loadDetail(selectedName, "groups");
  }

  function updatePublishForm(form) {
    if (state.state !== "writing" && state.error) {
      setState((current) => ({ ...current, error: "" }));
    }
    setPublishDialog((current) => ({ ...current, form, error: "" }));
  }

  function updateOffsetForm(form) {
    if (state.state !== "writing" && state.error) {
      setState((current) => ({ ...current, error: "" }));
    }
    setOffsetDialog((current) => ({ ...current, form, error: "" }));
  }

  if (!activeSession.active) {
    return (
      <div className={`grid h-full min-h-0 place-items-center p-6 ${panelClass}`}>
        <div className="grid max-w-lg gap-4 text-center">
          <Database className={`mx-auto h-8 w-8 ${mutedClass}`} />
          <div>
            <p className="font-semibold">No active {product} session</p>
            <p className={`mt-1 text-sm ${mutedClass}`}>
              Start a structured session to browse topics, consumer groups, lag, and bounded message samples.
            </p>
          </div>
          <Button type="button" className="mx-auto" onClick={onNewStructuredSession}>
            New session
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
      <div className="grid min-h-0 gap-4 overflow-y-auto p-4 xl:grid-cols-[360px_minmax(0,1fr)] xl:overflow-hidden">
        <section
          className={`grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${borderClass} ${subtlePanelClass}`}
        >
          <div className={`grid grid-cols-2 gap-1 border-b p-2 ${borderClass}`} role="tablist" aria-label={`${product} browser view`}>
            <Button
              type="button"
              role="tab"
              aria-selected={view === "topics"}
              variant={view === "topics" ? "default" : "outline"}
              className="h-9"
              onClick={() => void changeView("topics")}
            >
              <Database className="h-4 w-4" />
              Topics
            </Button>
            <Button
              type="button"
              role="tab"
              aria-selected={view === "groups"}
              variant={view === "groups" ? "default" : "outline"}
              className="h-9"
              onClick={() => void changeView("groups")}
            >
              <Users className="h-4 w-4" />
              Groups
            </Button>
          </div>
          <div className={`grid grid-cols-[minmax(0,1fr)_auto] gap-2 border-b p-3 ${borderClass}`}>
            <div className="relative">
              <Search className={`pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 ${mutedClass}`} />
              <Input
                className={`pl-9 ${inputClass}`}
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder={`Filter ${view}`}
                aria-label={`Filter ${view}`}
              />
            </div>
            <Button
              type="button"
              variant="outline"
              className="h-9 w-9 px-0"
              title={`Refresh ${view}`}
              onClick={() => void refreshList()}
              disabled={state.state !== "idle"}
            >
              <RefreshCcw className="h-4 w-4" />
            </Button>
          </div>
          <div className="min-h-0 overflow-auto p-2">
            {filteredItems.map((item) => (
              <button
                key={item.name}
                type="button"
                className={`mb-1 grid w-full gap-1 rounded-md border px-3 py-2 text-left transition ${selectedName === item.name ? activeClass : `${borderClass} ${hoverClass}`}`}
                onClick={() => void selectItem(item)}
                aria-pressed={selectedName === item.name}
              >
                <span className="truncate font-mono text-xs font-semibold" title={item.name}>
                  {item.name}
                </span>
                <span className={`truncate text-xs ${selectedName === item.name ? "" : mutedClass}`}>
                  {view === "topics"
                    ? `${item.partition_count || 0} partitions · replication ${item.replication_factor || 0}`
                    : `${item.state || "unknown"} · ${item.protocol_type || "unknown"}`}
                </span>
              </button>
            ))}
            {filteredItems.length === 0 ? <Notice>{state.state === "loading" ? `Loading ${view}...` : `No ${view} found.`}</Notice> : null}
          </div>
        </section>

        <section className={`grid min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-lg border ${borderClass}`}>
          <div className={`flex items-center justify-between gap-3 border-b p-3 ${borderClass} ${subtlePanelClass}`}>
            <div className="min-w-0">
              <p className="truncate text-sm font-semibold">
                {selectedName || (view === "topics" ? "Topic detail" : "Consumer group detail")}
              </p>
              <p className={`truncate text-xs ${mutedClass}`}>
                {selectedName
                  ? view === "topics"
                    ? "Partitions, offsets, and bounded message samples"
                    : "Members, assignments, committed offsets, and lag"
                  : `Select one of the ${view} on the left.`}
              </p>
            </div>
            <div className="flex items-center gap-2">
              {activeDetail && view === "topics" ? (
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 px-2 text-xs"
                  onClick={openPublishDialog}
                  disabled={state.state !== "idle"}
                >
                  <Send className="h-3.5 w-3.5" />
                  Publish
                </Button>
              ) : null}
              {activeDetail && view === "groups" && offsetPartitions.length > 0 ? (
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 px-2 text-xs"
                  onClick={openOffsetDialog}
                  disabled={state.state !== "idle"}
                >
                  <Gauge className="h-3.5 w-3.5" />
                  Set offset
                </Button>
              ) : null}
              {activeDetail ? (
                <CopyButton
                  value={JSON.stringify({ detail: activeDetail, messages }, null, 2)}
                  variant="outline"
                  className="h-8 px-2 text-xs"
                >
                  JSON
                </CopyButton>
              ) : null}
            </div>
          </div>
          <div className="grid min-h-0 gap-4 overflow-y-auto p-4 lg:grid-cols-2 lg:overflow-hidden">
            <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
              <p className={`text-xs font-semibold uppercase ${mutedClass}`}>{view === "topics" ? "Topic metadata" : "Group and lag"}</p>
              <TerminalBlock surface="log" className="min-h-0 text-xs">
                {activeDetail ? JSON.stringify(activeDetail, null, 2) : "No item selected."}
              </TerminalBlock>
            </div>
            <div className="grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] gap-2">
              <p className={`text-xs font-semibold uppercase ${mutedClass}`}>{view === "topics" ? "Message sample" : "Assignments"}</p>
              {view === "topics" ? (
                <div className="grid gap-2 sm:grid-cols-[100px_140px_110px_minmax(0,1fr)_auto]">
                  <Select
                    className={inputClass}
                    value={readForm.partition}
                    onChange={(event) => setReadForm({ ...readForm, partition: event.target.value })}
                    aria-label="Partition"
                  >
                    {(activeDetail?.partitions || []).map((partition) => (
                      <option value={partition.partition} key={partition.partition}>
                        p{partition.partition}
                      </option>
                    ))}
                  </Select>
                  <Select
                    className={inputClass}
                    value={readForm.start_position}
                    onChange={(event) => setReadForm({ ...readForm, start_position: event.target.value })}
                    aria-label="Start position"
                  >
                    <option value="recent">Recent</option>
                    <option value="earliest">Earliest</option>
                    <option value="offset">Offset</option>
                  </Select>
                  <Input
                    className={inputClass}
                    type="number"
                    min="0"
                    value={readForm.start_position === "offset" ? readForm.offset : readForm.max_records}
                    onChange={(event) =>
                      setReadForm({ ...readForm, [readForm.start_position === "offset" ? "offset" : "max_records"]: event.target.value })
                    }
                    aria-label={readForm.start_position === "offset" ? "Offset" : "Maximum records"}
                  />
                  <span />
                  <Button
                    type="button"
                    className="h-9"
                    disabled={!selectedName || state.state !== "idle"}
                    onClick={() => void readMessages()}
                  >
                    <Eye className="h-4 w-4" />
                    Read
                  </Button>
                </div>
              ) : (
                <span />
              )}
              <TerminalBlock surface="log" className="min-h-0 text-xs">
                {view === "topics"
                  ? messages
                    ? JSON.stringify(messages, null, 2)
                    : "No messages read in this session."
                  : activeDetail
                    ? JSON.stringify({ members: activeDetail.members || [], partitions: activeDetail.partitions || [] }, null, 2)
                    : "No consumer group selected."}
              </TerminalBlock>
            </div>
          </div>
          <div className={`grid gap-2 border-t p-3 ${borderClass}`} aria-live="polite">
            <Notice tone="warn">
              Message values, keys, and headers can contain secrets. Samples are bounded and never commit consumer offsets.
            </Notice>
            <Notice tone="warn">
              Publishing writes one message. Offset changes can replay or skip messages. Local browser writes require confirmation here; for
              MCP access, keep both actions on Prompt unless direct execution is intentional.
            </Notice>
            {state.error ? <Notice tone="bad">{state.error}</Notice> : null}
            {state.message ? <Notice tone="good">{state.message}</Notice> : null}
          </div>
        </section>
      </div>
      <div className={`flex min-w-0 items-center justify-between gap-3 border-t px-3 py-2 text-xs ${borderClass}`}>
        <span className={`truncate font-mono ${mutedClass}`}>{target.ref}</span>
        <span className={`flex items-center gap-2 truncate ${mutedClass}`}>
          {latestAction ? (
            <Badge tone={latestAction.status === "completed" ? "good" : latestAction.status === "failed" ? "bad" : "warn"}>
              {latestAction.action_name}
            </Badge>
          ) : (
            <Activity className="h-3.5 w-3.5" />
          )}
          {String(target.config?.bootstrap_brokers || "")
            .split(/[\s,]+/)
            .filter(Boolean)
            .join(", ")}
        </span>
      </div>
      <KafkaPublishDialog
        value={publishDialog}
        theme={theme}
        product={product}
        topic={selectedName}
        partitions={activeDetail?.partitions || []}
        pending={state.state === "writing"}
        actionError={state.error}
        onChange={updatePublishForm}
        onClose={() => setPublishDialog({ open: false, form: defaultPublish, error: "" })}
        onConfirm={() => void publishMessage()}
      />
      <KafkaOffsetDialog
        value={offsetDialog}
        theme={theme}
        product={product}
        group={selectedName}
        partitions={offsetPartitions}
        pending={state.state === "writing"}
        actionError={state.error}
        onChange={updateOffsetForm}
        onClose={() => setOffsetDialog({ open: false, form: defaultOffset, error: "" })}
        onConfirm={() => void setConsumerGroupOffset()}
      />
    </div>
  );
}
