import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { useKafkaBrowser } from "./use-kafka-browser";
import { useKafkaWrites } from "./use-kafka-writes";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn() }));

const topics = [
  { name: "orders", partition_count: 2 },
  { name: "events", partition_count: 1 },
];

beforeEach(() => {
  apiPost.mockReset();
  apiPost.mockImplementation(async (_path, payload) => completed(payload.action_name, responseFor(payload.action_name, payload.input)));
});

function useHarness() {
  const browser = useKafkaBrowser({
    target: { ref: "kafka:1:1", config: { server_family: "kafka" } },
    approvals: { data: [] },
    session: { active: true, startedAt: "now" },
    onRefreshActivity: vi.fn(),
  });
  return { browser, writes: useKafkaWrites({ browser }) };
}

it("loads Kafka topics and ignores detail from a superseded selection", async () => {
  const pending = new Map();
  apiPost.mockImplementation((_path, payload) => {
    if (payload.action_name !== "describe_topic") return Promise.resolve(completed(payload.action_name, responseFor(payload.action_name, payload.input)));
    return new Promise((resolve) => pending.set(payload.input.topic, resolve));
  });
  const { result } = renderHook(useHarness);
  await waitFor(() => expect(result.current.browser.filteredItems).toHaveLength(2));
  act(() => void result.current.browser.selectItem(topics[0]));
  await waitFor(() => expect(pending.has("orders")).toBe(true));
  act(() => void result.current.browser.selectItem(topics[1]));
  await waitFor(() => expect(pending.has("events")).toBe(true));

  await act(async () => pending.get("orders")(completed("describe_topic", { name: "orders", partitions: [] })));
  expect(result.current.browser.activeDetail).toBeNull();
  await act(async () => pending.get("events")(completed("describe_topic", { name: "events", partitions: [{ partition: 0 }] })));
  await waitFor(() => expect(result.current.browser.activeDetail?.name).toBe("events"));
});

it("rejects malformed Kafka publish headers before the mutation is dispatched", async () => {
  const { result } = renderHook(useHarness);
  await waitFor(() => expect(result.current.browser.filteredItems).toHaveLength(2));
  await act(async () => result.current.browser.selectItem(topics[0]));
  act(() => {
    result.current.writes.openPublishDialog();
    result.current.writes.updatePublishForm({
      partition: "0",
      key: "",
      key_encoding: "utf8",
      value: "hello",
      value_encoding: "utf8",
      headers: "{}",
    });
  });
  apiPost.mockClear();
  await act(async () => result.current.writes.publishMessage());

  expect(result.current.writes.publishDialog.error).toBe("Headers must be a JSON array.");
  expect(apiPost).not.toHaveBeenCalled();
});

function completed(actionName, output) {
  return { id: 1, status: "completed", action_name: actionName, output };
}

function responseFor(actionName, input) {
  if (actionName === "list_topics") return { topics };
  if (actionName === "describe_topic") return { name: input.topic, partitions: [{ partition: 0 }] };
  return {};
}
