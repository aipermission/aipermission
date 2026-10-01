import { expect, it } from "vitest";
import {
  filterQueues,
  formatMessages,
  parsePublishProperties,
  queueMetaText,
  queueNameLabel,
  queueTotals,
  readRabbitQueues,
  readRabbitRecords,
  uniqueQueueNames,
} from "./helpers";

it.each([
  [" ", '"\\u0020"'],
  ["  ", '"\\u0020\\u0020"'],
  ["\u00a0", '"\\u00a0"'],
  ["\t", '"\\t"'],
  ["\\u0020", '"\\\\u0020"'],
  ['" ', '"\\"\\u0020"'],
  ["jobs", '"jobs"'],
])("renders an unambiguous display label for %j", (name, label) => {
  expect(queueNameLabel(name)).toBe(label);
});

it("validates queue identities while retaining connector metadata", () => {
  const queues = readRabbitQueues([
    null,
    [],
    { name: 1 },
    { name: "jobs", state: 3, vhost: false, messages: 4 },
    { name: "failed", state: "running" },
  ]);
  expect(queues).toHaveLength(2);
  expect(queues[0]).toMatchObject({ name: "jobs", state: undefined, vhost: undefined, messages: 4 });
  expect(filterQueues(queues, " FAIL ").map((queue) => queue.name)).toEqual(["failed"]);
  expect(filterQueues(queues, "")).toBe(queues);
  expect(readRabbitQueues({ queues })).toEqual([]);
  expect(uniqueQueueNames([{ name: "jobs" }, { name: "jobs" }, { name: "" }])).toEqual(["jobs"]);
  expect(uniqueQueueNames(null)).toEqual([]);
});

it("normalizes absent counters and excludes malformed record collections", () => {
  expect(queueTotals([{ messages: "3", messages_ready: 2, consumers: "invalid" }])).toEqual({
    messages: 3,
    ready: 2,
    unacked: 0,
    consumers: 0,
  });
  expect(queueTotals(null).messages).toBe(0);
  expect(queueMetaText({})).toContain("ready 0");
  expect(readRabbitRecords([null, [], "text", { payload: "a" }])).toEqual([{ payload: "a" }]);
  expect(readRabbitRecords(null)).toEqual([]);
});

it("deduplicates only exact queue identities without coercing malformed names", () => {
  const input = JSON.parse(
    '[{"name":" jobs "},{"name":"jobs"},{"name":" "},{"name":" jobs "},{"name":""},{"name":42},{"name":true},{"name":{}},{"name":null},{}]',
  );
  expect(uniqueQueueNames(input)).toHaveLength(3);
  expect(uniqueQueueNames(input)).toEqual(expect.arrayContaining([" jobs ", "jobs", " "]));
  expect(readRabbitQueues(input).map((queue) => queue.name)).toEqual([" jobs ", "jobs", " ", " jobs ", ""]);
});

it("formats payloads without treating external message data as instructions", () => {
  const messages = JSON.parse(formatMessages([{ payload: '{"a":1}' }, { payload: "plain" }, { payload: " " }, { payload: 3 }]));
  expect(messages.map((message: { payload: unknown }) => message.payload)).toEqual([{ a: 1 }, "plain", " ", 3]);
  expect(parsePublishProperties("")).toEqual({ value: {}, error: "" });
  expect(parsePublishProperties('{"content_type":"application/json"}').error).toBe("");
  for (const value of ["null", "[]", "true", "bad"]) expect(parsePublishProperties(value).error).toBe("Properties must be a JSON object.");
});
