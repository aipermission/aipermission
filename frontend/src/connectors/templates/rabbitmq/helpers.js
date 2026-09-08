export function filterQueues(queues, pattern) {
  const needle = String(pattern || "")
    .trim()
    .toLowerCase();
  if (!needle) return queues;
  return queues.filter((queue) =>
    String(queue.name || "")
      .toLowerCase()
      .includes(needle),
  );
}

export function uniqueQueueNames(queues) {
  return Array.from(new Set((queues || []).map((queue) => String(queue.name || "").trim()).filter(Boolean))).sort((left, right) =>
    left.localeCompare(right),
  );
}

export function queueMetaText(queue) {
  return `ready ${numberText(queue.messages_ready)} · unacked ${numberText(queue.messages_unacknowledged)} · consumers ${numberText(queue.consumers)} · durable ${queue.durable ? "yes" : "no"}`;
}

export function formatMessages(messages) {
  return JSON.stringify(
    messages.map((message, index) => ({
      index: index + 1,
      payload: formatPayload(message.payload),
      payload_encoding: message.payload_encoding,
      redelivered: message.redelivered,
      properties: message.properties,
    })),
    null,
    2,
  );
}

export function queueTotals(queues) {
  return (queues || []).reduce(
    (totals, queue) => ({
      ready: totals.ready + numericValue(queue.messages_ready),
      unacked: totals.unacked + numericValue(queue.messages_unacknowledged),
      messages: totals.messages + numericValue(queue.messages),
      consumers: totals.consumers + numericValue(queue.consumers),
    }),
    { ready: 0, unacked: 0, messages: 0, consumers: 0 },
  );
}

export function parsePublishProperties(value) {
  if (!String(value || "").trim()) return { value: {}, error: "" };
  try {
    const parsed = JSON.parse(value);
    if (!parsed || Array.isArray(parsed) || typeof parsed !== "object") return { value: null, error: "Properties must be a JSON object." };
    return { value: parsed, error: "" };
  } catch {
    return { value: null, error: "Properties must be a JSON object." };
  }
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
