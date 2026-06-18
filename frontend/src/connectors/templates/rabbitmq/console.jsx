import { Database, Eye, ListTree, RefreshCcw, Search, Send } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Input, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { apiPost } from "../../../lib/api";

const defaultQueueLimit = 250;
const defaultPeekCount = 5;
const defaultPayloadBytes = 65536;

export function RabbitMQConnectorConsoleTemplate({ target, approvals, theme, session, onNewStructuredSession, onRefreshActivity }) {
  const activeSession = session || { active: false, startedAt: "" };
  const [pattern, setPattern] = useState("");
  const [vhost, setVhost] = useState(target.config?.vhost || "/");
  const [queues, setQueues] = useState([]);
  const [activeQueue, setActiveQueue] = useState("");
  const [queueDetail, setQueueDetail] = useState(null);
  const [bindings, setBindings] = useState([]);
  const [messages, setMessages] = useState([]);
  const [peekCount, setPeekCount] = useState(defaultPeekCount);
  const [detailMode, setDetailMode] = useState("inspect");
  const [publishExchange, setPublishExchange] = useState("amq.default");
  const [customRoutingKey, setCustomRoutingKey] = useState(false);
  const [publishRoutingKey, setPublishRoutingKey] = useState("");
  const [publishPayload, setPublishPayload] = useState("");
  const [publishProperties, setPublishProperties] = useState('{"content_type":"application/json"}');
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const panelClass = theme === "light" ? "bg-white text-stone-900" : "bg-[#1e1e1e] text-stone-100";
  const mutedClass = theme === "light" ? "text-stone-500" : "text-stone-400";
  const borderClass = theme === "light" ? "border-stone-200" : "border-stone-700";
  const subtlePanelClass = theme === "light" ? "bg-stone-50" : "bg-[#252526]";
  const inputClass = theme === "light" ? "border-stone-300 bg-white text-stone-900 placeholder:text-stone-400" : "border-stone-700 bg-[#1a1a1a] text-stone-100 placeholder:text-stone-500";
  const rowHoverClass = theme === "light" ? "hover:bg-stone-50" : "hover:bg-stone-800/60";
  const activeRowClass = theme === "light" ? "border-emerald-200 bg-emerald-50 text-emerald-950" : "border-emerald-700 bg-emerald-950/40 text-emerald-100";
  const activeItems = useMemo(() => (approvals?.data || []).filter((item) => item.target_ref === target.ref), [approvals?.data, target.ref]);
  const latestAction = activeItems[0] || null;
  const filteredQueues = useMemo(() => filterQueues(queues, pattern), [queues, pattern]);

  useEffect(() => {
    setVhost(target.config?.vhost || "/");
    setQueues([]);
    setActiveQueue("");
    setQueueDetail(null);
    setBindings([]);
    setMessages([]);
    setPattern("");
    setPeekCount(defaultPeekCount);
    setDetailMode("inspect");
    setPublishExchange("amq.default");
    setCustomRoutingKey(false);
    setPublishRoutingKey("");
    setPublishPayload("");
    setPublishProperties('{"content_type":"application/json"}');
  }, [target.ref, activeSession.active, activeSession.startedAt]);

  useEffect(() => {
    if (!activeSession.active) return;
    void refreshQueues();
  }, [activeSession.active, activeSession.startedAt, target.ref]);

  async function runRabbitAction({ actionName, input, reason, busy = "running", suppressError = false }) {
    setState({ state: busy, error: "", message: "" });
    try {
      const item = await apiPost("/api/connector-actions/local-run", {
        target_ref: target.ref,
        action_name: actionName,
        input,
        reason,
      });
      setState({ state: "idle", error: "", message: item.display_text || "" });
      await onRefreshActivity?.();
      return item;
    } catch (error) {
      if (suppressError) {
        setState({ state: "idle", error: "", message: "" });
      } else {
        setState({ state: "error", error: error.message || "RabbitMQ action failed.", message: "" });
      }
      throw error;
    }
  }

  async function refreshQueues() {
    if (!activeSession.active) return;
    const queueItem = await runRabbitAction({
      actionName: "list_queues",
      input: { vhost, pattern: "", limit: defaultQueueLimit },
      reason: "manual RabbitMQ browser queue list",
      busy: "loading",
    });
    const nextQueues = Array.isArray(queueItem.output?.queues) ? queueItem.output.queues : [];
    setQueues(nextQueues);
    if (activeQueue && !nextQueues.some((queue) => queue.name === activeQueue)) {
      setActiveQueue("");
      setQueueDetail(null);
      setBindings([]);
      setMessages([]);
    }
  }

  async function selectQueue(queueName) {
    if (!activeSession.active || !queueName) return;
    setActiveQueue(queueName);
    setDetailMode("inspect");
    setCustomRoutingKey(false);
    setPublishRoutingKey(queueName);
    setMessages([]);
    const detailItem = await runRabbitAction({ actionName: "get_queue", input: { vhost, queue: queueName }, reason: "manual RabbitMQ browser queue detail", busy: "reading" });
    setQueueDetail(detailItem.output || null);
    try {
      const bindingItem = await runRabbitAction({ actionName: "list_bindings", input: { vhost, queue: queueName, limit: 250 }, reason: "manual RabbitMQ browser queue bindings", busy: "reading", suppressError: true });
      setBindings(Array.isArray(bindingItem.output?.bindings) ? bindingItem.output.bindings : []);
    } catch {
      setBindings([]);
    }
  }

  async function peekMessages() {
    if (!activeQueue) return;
    const item = await runRabbitAction({
      actionName: "peek_messages",
      input: { vhost, queue: activeQueue, count: Number(peekCount) || defaultPeekCount, max_payload_bytes: defaultPayloadBytes },
      reason: "manual RabbitMQ browser message peek",
      busy: "peeking",
    });
    setMessages(Array.isArray(item.output?.messages) ? item.output.messages : []);
  }

  function startPublish() {
    setDetailMode("publish");
    setState({ state: "idle", error: "", message: "" });
    if (!publishRoutingKey.trim() && activeQueue) {
      setCustomRoutingKey(false);
      setPublishRoutingKey(activeQueue);
    }
  }

  async function publishMessage(event) {
    event.preventDefault();
    if (!activeSession.active || state.state !== "idle") return;
    const routingKey = publishRoutingKey.trim();
    if (!routingKey || !publishPayload) {
      setState({ state: "error", error: "Routing key and payload are required.", message: "" });
      return;
    }
    let properties = {};
    if (publishProperties.trim()) {
      try {
        properties = JSON.parse(publishProperties);
      } catch {
        setState({ state: "error", error: "Properties must be a JSON object.", message: "" });
        return;
      }
      if (!properties || Array.isArray(properties) || typeof properties !== "object") {
        setState({ state: "error", error: "Properties must be a JSON object.", message: "" });
        return;
      }
    }
    await runRabbitAction({
      actionName: "publish_message",
      input: {
        vhost,
        exchange: publishExchange.trim() || "amq.default",
        routing_key: routingKey,
        payload: publishPayload,
        payload_encoding: "string",
        properties,
      },
      reason: "manual RabbitMQ browser message publish",
      busy: "publishing",
    });
    setPublishPayload("");
    await refreshQueues();
  }

  if (!activeSession.active) {
    return (
      <div className={`grid min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
        <div className="grid place-items-center p-8 text-center">
          <div className="grid max-w-lg gap-4">
            <Database className={`mx-auto h-10 w-10 ${mutedClass}`} />
            <div>
              <h3 className="text-lg font-semibold">No active RabbitMQ session</h3>
              <p className={`mt-2 text-sm ${mutedClass}`}>Start a structured session to browse queues through the connector approval, history, and audit pipeline.</p>
            </div>
            <Button type="button" className="mx-auto" onClick={onNewStructuredSession}>
              Start RabbitMQ session
            </Button>
          </div>
        </div>
        <RabbitEndpointFooter target={target} borderClass={borderClass} mutedClass={mutedClass} />
      </div>
    );
  }

  return (
    <div className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
      <div className="grid min-h-0 gap-4 overflow-hidden p-4 xl:grid-cols-[360px_minmax(0,1fr)]">
        <section className={`grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden rounded-lg border ${borderClass} ${subtlePanelClass}`}>
          <div className={`border-b p-3 ${borderClass}`}>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <p className="text-sm font-semibold">Queues</p>
                <p className={`text-xs ${mutedClass}`}>{filteredQueues.length} shown · {queues.length} loaded</p>
              </div>
              <div className="flex items-center gap-2">
                {latestAction ? <Badge tone={latestAction.status === "failed" ? "bad" : latestAction.status === "completed" ? "good" : "warn"}>{latestAction.action_name}</Badge> : null}
                <Button type="button" variant="outline" className="h-8 w-8 px-0" title="Refresh queues" onClick={refreshQueues} disabled={state.state !== "idle"}>
                  <RefreshCcw className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>
          </div>
          <form
            className={`grid gap-2 border-b p-3 ${borderClass}`}
            onSubmit={(event) => {
              event.preventDefault();
              void refreshQueues();
            }}
          >
            <div className="relative">
              <Search className={`pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 ${mutedClass}`} />
              <Input className={`pl-9 ${inputClass}`} value={pattern} onChange={(event) => setPattern(event.target.value)} placeholder="Filter queues" />
            </div>
            <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-2">
              <Input className={inputClass} value={vhost} onChange={(event) => setVhost(event.target.value)} placeholder="vhost" />
              <Button type="submit" variant="outline" className="h-9" disabled={state.state !== "idle"}>
                {state.state === "loading" ? "Loading" : "Refresh"}
              </Button>
            </div>
          </form>
          <div className="min-h-0 overflow-auto p-2">
            {filteredQueues.map((queue) => (
              <button
                key={`${queue.vhost || vhost}:${queue.name}`}
                type="button"
                className={`mb-1 grid w-full gap-1 rounded-md border px-3 py-2 text-left text-sm transition ${activeQueue === queue.name ? activeRowClass : `${borderClass} ${rowHoverClass}`}`}
                onClick={() => selectQueue(queue.name)}
              >
                <span className="truncate font-mono text-xs font-semibold" title={queue.name}>{queue.name}</span>
                <span className={`text-xs ${activeQueue === queue.name ? "" : mutedClass}`}>
                  ready {numberText(queue.messages_ready)} · unacked {numberText(queue.messages_unacknowledged)} · consumers {numberText(queue.consumers)}
                </span>
              </button>
            ))}
            {filteredQueues.length === 0 ? <Notice>{state.state === "loading" ? "Loading RabbitMQ queues..." : "No queues found for this vhost/filter."}</Notice> : null}
          </div>
          <div className={`border-t p-3 ${borderClass}`}>
            <QueueTotalsStrip queues={queues} mutedClass={mutedClass} />
          </div>
        </section>

        <section className={`grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden rounded-lg border ${borderClass}`}>
          <div className={`flex flex-wrap items-center justify-between gap-3 border-b p-3 ${borderClass} ${subtlePanelClass}`}>
            <div className="min-w-0">
              <p className="text-sm font-semibold">{detailMode === "publish" ? "Publish message" : activeQueue || "Queue detail"}</p>
              <p className={`truncate text-xs ${mutedClass}`}>
                {detailMode === "publish"
                  ? publishHelpText(activeQueue)
                  : queueDetail
                    ? queueMetaText(queueDetail)
                    : "Select a queue to inspect counters, bindings, and bounded message previews."}
              </p>
            </div>
            <div className="flex items-center gap-2">
              {detailMode === "inspect" && queueDetail?.state ? <Badge tone={queueDetail.state === "running" ? "good" : "warn"}>{queueDetail.state}</Badge> : null}
              {detailMode === "inspect" && queueDetail ? <CopyButton value={JSON.stringify({ queue: queueDetail, bindings, messages }, null, 2)} variant="outline" className="h-8 px-2 text-xs" title="Copy queue JSON">JSON</CopyButton> : null}
            </div>
          </div>
          <div className={`flex flex-wrap items-center justify-between gap-2 border-b p-3 ${borderClass}`}>
            <div className="flex min-w-0 items-center gap-2">
              {detailMode === "publish" ? (
                <>
                  <Send className={`h-4 w-4 ${mutedClass}`} />
                  <span className={`text-xs ${mutedClass}`}>New write action</span>
                </>
              ) : (
                <>
                  <ListTree className={`h-4 w-4 ${mutedClass}`} />
                  <span className={`text-xs ${mutedClass}`}>{bindings.length} binding(s)</span>
                </>
              )}
            </div>
            <div className="flex flex-wrap items-center justify-end gap-2">
              {detailMode === "publish" ? (
                <Button type="button" variant="outline" className="h-8 px-3 text-xs" onClick={() => setDetailMode("inspect")}>
                  Back to detail
                </Button>
              ) : (
                <>
                  <Input
                    className={`h-8 w-24 ${inputClass}`}
                    type="number"
                    min="1"
                    max="50"
                    value={peekCount}
                    onChange={(event) => setPeekCount(event.target.value)}
                    aria-label="Peek count"
                  />
                  <Button type="button" className="h-8 px-3 text-xs" disabled={!activeQueue || state.state !== "idle"} onClick={peekMessages}>
                    <Eye className="h-3.5 w-3.5" />
                    Peek
                  </Button>
                  <Button type="button" variant="outline" className="h-8 px-3 text-xs" disabled={state.state !== "idle"} onClick={startPublish}>
                    <Send className="h-3.5 w-3.5" />
                    Publish
                  </Button>
                </>
              )}
            </div>
          </div>
          <div className="min-h-0 overflow-hidden p-4">
            {detailMode === "publish" ? (
              <form className="grid h-full min-h-0 grid-rows-[auto_auto_auto_minmax(0,1fr)_auto] gap-3" onSubmit={publishMessage}>
                <Notice tone="warn">
                  This creates a new RabbitMQ message. With the default exchange, set Routing key to the destination queue name.
                </Notice>
                <div className="grid gap-2 md:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)]">
                  <FieldBlock label="Exchange" help="amq.default is RabbitMQ's default exchange. It routes directly to the queue named by the routing key." mutedClass={mutedClass}>
                    <Input className={inputClass} value={publishExchange} onChange={(event) => setPublishExchange(event.target.value)} placeholder="amq.default" aria-label="Publish exchange" />
                  </FieldBlock>
                  <FieldBlock label="Routing key" help={activeQueue ? `Use ${activeQueue} to publish to the selected queue via amq.default.` : "Usually the queue name when using amq.default."} mutedClass={mutedClass}>
                    <div className={`grid gap-2 ${customRoutingKey ? "md:grid-cols-2" : ""}`}>
                      <RoutingKeyPicker
                        queues={queues}
                        value={publishRoutingKey}
                        custom={customRoutingKey}
                        onQueue={(queueName) => {
                          setCustomRoutingKey(false);
                          setPublishRoutingKey(queueName);
                        }}
                        onCustom={() => {
                          setCustomRoutingKey(true);
                          if (!publishRoutingKey.trim()) {
                            setPublishRoutingKey("");
                          }
                        }}
                        inputClass={inputClass}
                        borderClass={borderClass}
                        mutedClass={mutedClass}
                        subtlePanelClass={subtlePanelClass}
                        rowHoverClass={rowHoverClass}
                        activeRowClass={activeRowClass}
                      />
                      {customRoutingKey ? (
                        <Input className={inputClass} value={publishRoutingKey} onChange={(event) => setPublishRoutingKey(event.target.value)} placeholder="Custom routing key" aria-label="Custom routing key" />
                      ) : null}
                    </div>
                  </FieldBlock>
                </div>
                <FieldBlock label="Properties JSON" help="Optional AMQP properties. content_type helps consumers parse JSON payloads." mutedClass={mutedClass}>
                  <Input className={inputClass} value={publishProperties} onChange={(event) => setPublishProperties(event.target.value)} placeholder='{"content_type":"application/json"}' aria-label="Publish properties JSON" />
                </FieldBlock>
                <FieldBlock label="Payload" help="Message body to publish. Keep secrets out unless this write was explicitly approved." mutedClass={mutedClass} grow>
                  <Textarea className={`h-full min-h-0 resize-none font-mono text-xs ${inputClass}`} value={publishPayload} onChange={(event) => setPublishPayload(event.target.value)} placeholder='{"type":"test","ok":true}' aria-label="Publish payload" />
                </FieldBlock>
                <div className="flex justify-end">
                  <Button type="submit" className="h-9 px-4 text-sm" disabled={state.state !== "idle" || !publishRoutingKey.trim() || !publishPayload}>
                    <Send className="h-4 w-4" />
                    Publish message
                  </Button>
                </div>
              </form>
            ) : (
              <div className="grid h-full min-h-0 gap-4 overflow-hidden lg:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)]">
                <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
                  <p className={`text-xs font-semibold uppercase ${mutedClass}`}>Queue and bindings</p>
                  <TerminalBlock surface="log" className="min-h-0 text-xs">{queueDetail ? JSON.stringify({ queue: queueDetail, bindings }, null, 2) : "No queue selected."}</TerminalBlock>
                </div>
                <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
                  <p className={`text-xs font-semibold uppercase ${mutedClass}`}>Messages</p>
                  <TerminalBlock surface="log" className="min-h-0 text-xs">{messages.length ? formatMessages(messages) : "No messages peeked in this session."}</TerminalBlock>
                </div>
              </div>
            )}
          </div>
          <div className={`grid gap-3 border-t p-3 ${borderClass}`}>
            <Notice tone="warn">Peek uses ack_requeue_true with bounded count and payload truncation. Avoid reading payloads unless the operator approved that access.</Notice>
            <Notice tone="warn">Publish creates a new RabbitMQ message and uses the write permission for this connector action.</Notice>
            {state.error ? <Notice tone="bad">{state.error}</Notice> : null}
            {state.message ? <Notice tone="good">{state.message}</Notice> : null}
          </div>
        </section>
      </div>
      <RabbitEndpointFooter target={target} borderClass={borderClass} mutedClass={mutedClass} />
    </div>
  );
}

function QueueTotalsStrip({ queues, mutedClass }) {
  const totals = (queues || []).reduce(
    (acc, queue) => ({
      ready: acc.ready + numericValue(queue.messages_ready),
      unacked: acc.unacked + numericValue(queue.messages_unacknowledged),
      messages: acc.messages + numericValue(queue.messages),
      consumers: acc.consumers + numericValue(queue.consumers),
    }),
    { ready: 0, unacked: 0, messages: 0, consumers: 0 }
  );
  return (
    <div className="grid gap-1 text-xs">
      <span>Loaded queue totals</span>
      <span className={mutedClass}>
        queues {queues.length} · messages {totals.messages} · ready {totals.ready} · unacked {totals.unacked} · consumers {totals.consumers}
      </span>
    </div>
  );
}

function RoutingKeyPicker({ queues, value, custom, onQueue, onCustom, inputClass, borderClass, mutedClass, subtlePanelClass, rowHoverClass, activeRowClass }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const queueNames = useMemo(() => uniqueQueueNames(queues), [queues]);
  const selectedLabel = custom ? "Custom routing key" : value || "";
  const visibleQueues = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return queueNames.slice(0, 80);
    return queueNames.filter((name) => name.toLowerCase().includes(needle)).slice(0, 80);
  }, [queueNames, query]);
  const options = useMemo(
    () => [
      { kind: "custom", label: "Custom routing key", help: "Type an exchange-specific routing key manually." },
      ...visibleQueues.map((queueName) => ({ kind: "queue", label: queueName, help: "Queue routing key via amq.default" })),
    ],
    [visibleQueues]
  );

  useEffect(() => {
    if (!open) return;
    setActiveIndex((index) => Math.min(Math.max(index, 0), Math.max(options.length - 1, 0)));
  }, [open, options.length]);

  function chooseCustom(event) {
    event.preventDefault();
    onCustom();
    setOpen(false);
    setQuery("");
  }

  function chooseQueue(event, queueName) {
    event.preventDefault();
    onQueue(queueName);
    setOpen(false);
    setQuery("");
  }

  function chooseOption(event, option) {
    if (!option) return;
    if (option.kind === "custom") {
      chooseCustom(event);
      return;
    }
    chooseQueue(event, option.label);
  }

  function handleKeyDown(event) {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setOpen(true);
      setActiveIndex((index) => (index + 1) % Math.max(options.length, 1));
      return;
    }
    if (event.key === "ArrowUp") {
      event.preventDefault();
      setOpen(true);
      setActiveIndex((index) => (index - 1 + Math.max(options.length, 1)) % Math.max(options.length, 1));
      return;
    }
    if ((event.key === "Enter" || event.key === "Tab") && open) {
      chooseOption(event, options[activeIndex]);
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      setOpen(false);
      setQuery("");
    }
  }

  return (
    <div className="relative">
      <Input
        aria-activedescendant={open ? `rabbit-routing-option-${activeIndex}` : undefined}
        aria-expanded={open}
        aria-haspopup="listbox"
        role="combobox"
        className={inputClass}
        value={open ? query : selectedLabel}
        onFocus={() => {
          setOpen(true);
          setQuery("");
          setActiveIndex(0);
        }}
        onBlur={() => {
          window.setTimeout(() => setOpen(false), 120);
        }}
        onChange={(event) => {
          setQuery(event.target.value);
          setOpen(true);
          setActiveIndex(0);
        }}
        onKeyDown={handleKeyDown}
        placeholder="Search queue routing keys"
        aria-label="Search queue routing keys"
      />
      {open ? (
        <div className={`absolute left-0 right-0 top-[calc(100%+4px)] z-20 max-h-64 overflow-auto rounded-md border p-1 shadow-xl ${borderClass} ${subtlePanelClass}`} role="listbox">
          {options.map((option, index) => (
            <button
              key={`${option.kind}:${option.label}`}
              id={`rabbit-routing-option-${index}`}
              type="button"
              role="option"
              aria-selected={index === activeIndex}
              className={`grid w-full gap-0.5 rounded px-2 py-2 text-left text-sm ${index === activeIndex ? activeRowClass : rowHoverClass}`}
              onMouseEnter={() => setActiveIndex(index)}
              onMouseDown={(event) => chooseOption(event, option)}
            >
              <span className={`${option.kind === "queue" ? "truncate font-mono text-xs" : ""} font-semibold`}>{option.label}</span>
              <span className={`text-xs ${mutedClass}`}>{option.help}</span>
            </button>
          ))}
          {visibleQueues.length === 0 ? <div className={`px-2 py-3 text-xs ${mutedClass}`}>No queue matches this search.</div> : null}
        </div>
      ) : null}
    </div>
  );
}

function FieldBlock({ label, help, mutedClass, children, grow = false }) {
  return (
    <label className={`grid min-h-0 gap-1 text-sm font-medium ${grow ? "grid-rows-[auto_minmax(0,1fr)_auto]" : ""}`}>
      <span>{label}</span>
      {children}
      {help ? <span className={`text-xs font-normal ${mutedClass}`}>{help}</span> : null}
    </label>
  );
}

function publishHelpText(activeQueue) {
  if (activeQueue) {
    return `Publishing to ${activeQueue} through amq.default uses routing_key=${activeQueue}.`;
  }
  return "Create one RabbitMQ message through the connector write permission.";
}

function RabbitEndpointFooter({ target, borderClass, mutedClass }) {
  return (
    <div className={`flex min-w-0 items-center justify-between gap-3 border-t px-3 py-2 text-xs ${borderClass}`}>
      <span className={`truncate font-mono ${mutedClass}`}>{target.ref}</span>
      <span className={`truncate ${mutedClass}`}>{target.config?.scheme || "http"}://{target.config?.host}:{target.config?.port || 15672} · vhost {target.config?.vhost || "/"}</span>
    </div>
  );
}

function filterQueues(queues, pattern) {
  const needle = String(pattern || "").trim().toLowerCase();
  if (!needle) return queues;
  return queues.filter((queue) => String(queue.name || "").toLowerCase().includes(needle));
}

function uniqueQueueNames(queues) {
  return Array.from(new Set((queues || []).map((queue) => String(queue.name || "").trim()).filter(Boolean))).sort((left, right) => left.localeCompare(right));
}

function queueMetaText(queue) {
  return `ready ${numberText(queue.messages_ready)} · unacked ${numberText(queue.messages_unacknowledged)} · consumers ${numberText(queue.consumers)} · durable ${queue.durable ? "yes" : "no"}`;
}

function formatMessages(messages) {
  return JSON.stringify(
    messages.map((message, index) => ({
      index: index + 1,
      payload: formatPayload(message.payload),
      payload_encoding: message.payload_encoding,
      redelivered: message.redelivered,
      properties: message.properties,
    })),
    null,
    2
  );
}

function formatPayload(payload) {
  if (typeof payload !== "string") return payload;
  const trimmed = payload.trim();
  if (!trimmed) return payload;
  try {
    return JSON.parse(trimmed);
  } catch {
    return payload;
  }
}

function numberText(value) {
  if (value === undefined || value === null || value === "") return "0";
  return String(value);
}

function numericValue(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number : 0;
}
