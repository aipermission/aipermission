import type { QueueCounters, RabbitMessage, RabbitQueue } from "./browser-types";

export function filterQueues(queues: RabbitQueue[], pattern: string) {
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

export function uniqueQueueNames(queues: readonly Pick<RabbitQueue, "name">[] | null | undefined) {
  return Array.from(new Set((queues || []).map((queue) => String(queue.name || "").trim()).filter(Boolean))).sort((left, right) =>
    left.localeCompare(right),
  );
}

export function queueMetaText(queue: QueueCounters) {
  return `ready ${numberText(queue.messages_ready)} · unacked ${numberText(queue.messages_unacknowledged)} · consumers ${numberText(queue.consumers)} · durable ${queue.durable ? "yes" : "no"}`;
}

export function formatMessages(messages: readonly RabbitMessage[]) {
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

export function queueTotals(queues: readonly QueueCounters[] | null | undefined) {
  return (queues || []).reduce<{ ready: number; unacked: number; messages: number; consumers: number }>(
    (totals, queue) => ({
      ready: totals.ready + numericValue(queue.messages_ready),
      unacked: totals.unacked + numericValue(queue.messages_unacknowledged),
      messages: totals.messages + numericValue(queue.messages),
      consumers: totals.consumers + numericValue(queue.consumers),
    }),
    { ready: 0, unacked: 0, messages: 0, consumers: 0 },
  );
}

export function parsePublishProperties(value: string): { value: Record<string, unknown>; error: "" } | { value: null; error: string } {
  if (!String(value || "").trim()) return { value: {}, error: "" };
  try {
    const parsed: unknown = JSON.parse(value);
    if (!isRabbitRecord(parsed)) return { value: null, error: "Properties must be a JSON object." };
    return { value: parsed, error: "" };
  } catch {
    return { value: null, error: "Properties must be a JSON object." };
  }
}

export function isRabbitRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

export function readRabbitQueue(value: unknown): RabbitQueue | null {
  if (!isRabbitRecord(value) || typeof value.name !== "string") return null;
  return {
    ...value,
    name: value.name,
    vhost: typeof value.vhost === "string" ? value.vhost : undefined,
    state: typeof value.state === "string" ? value.state : undefined,
  };
}

export function readRabbitQueues(value: unknown): RabbitQueue[] {
  return Array.isArray(value) ? value.map(readRabbitQueue).filter((queue) => queue !== null) : [];
}

export function readRabbitRecords(value: unknown): Record<string, unknown>[] {
  return Array.isArray(value) ? value.filter(isRabbitRecord) : [];
}

function formatPayload(payload: unknown): unknown {
  if (typeof payload !== "string") return payload;
  const trimmed = payload.trim();
  if (!trimmed) return payload;
  try {
    return JSON.parse(trimmed);
  } catch {
    return payload;
  }
}

function numberText(value: unknown) {
  if (value === undefined || value === null || value === "") return "0";
  return String(value);
}

function numericValue(value: unknown) {
  const number = Number(value);
  return Number.isFinite(number) ? number : 0;
}
