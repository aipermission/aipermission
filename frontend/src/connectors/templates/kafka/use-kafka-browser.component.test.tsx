import { connectorActionRequest } from "../../../test/connector-action-fixtures";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api.ts";
import { useKafkaBrowser } from "./use-kafka-browser";
import { useKafkaWrites } from "./use-kafka-writes";
import { connectorActionFixture } from "../../../test/connector-action-fixtures";
import type { ConnectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import type { KafkaBrowserProps } from "./console-types";

vi.mock("../../../lib/api.ts", () => ({ apiPost: vi.fn() }));

const topics = [
  { name: "orders", partition_count: 2 },
  { name: "events", partition_count: 1 },
];

beforeEach(() => {
  vi.mocked(apiPost).mockReset();
  vi.mocked(apiPost).mockImplementation(async (_path, payload) =>
    completed(
      connectorActionRequest(payload).action_name,
      responseFor(connectorActionRequest(payload).action_name, connectorActionRequest(payload).input),
    ),
  );
});

function useHarness(overrides: Partial<KafkaBrowserProps> = {}) {
  const browser = useKafkaBrowser({
    target: { ref: "kafka:1:1", config: { server_family: "kafka" } },
    approvals: { data: [] },
    session: { active: true, startedAt: "now" },
    onRefreshActivity: vi.fn(),
    ...overrides,
  });
  return { browser, writes: useKafkaWrites({ browser }) };
}

it("loads Kafka topics and ignores detail from a superseded selection", async () => {
  const pending = new Map<string, (_response: ConnectorActionResponse) => void>();
  vi.mocked(apiPost).mockImplementation((_path, payload) => {
    if (connectorActionRequest(payload).action_name !== "describe_topic")
      return Promise.resolve(
        completed(
          connectorActionRequest(payload).action_name,
          responseFor(connectorActionRequest(payload).action_name, connectorActionRequest(payload).input),
        ),
      );
    return new Promise((resolve) => pending.set(String(connectorActionRequest(payload).input.topic), resolve));
  });
  const { result } = renderHook(useHarness);
  await waitFor(() => expect(result.current.browser.filteredItems).toHaveLength(2));
  act(() => void result.current.browser.selectItem(topics[0]));
  await waitFor(() => expect(pending.has("orders")).toBe(true));
  act(() => void result.current.browser.selectItem(topics[1]));
  await waitFor(() => expect(pending.has("events")).toBe(true));

  await act(async () => pending.get("orders")?.(completed("describe_topic", { name: "orders", partitions: [] })));
  expect(result.current.browser.activeDetail).toBeNull();
  await act(async () => pending.get("events")?.(completed("describe_topic", { name: "events", partitions: [{ partition: 0 }] })));
  await waitFor(() => expect(result.current.browser.activeDetail?.name).toBe("events"));
});

it("does not commit messages after the selected topic changes", async () => {
  let resolveMessages: ((_response: ConnectorActionResponse) => void) | undefined;
  vi.mocked(apiPost).mockImplementation((_path, payload) => {
    if (connectorActionRequest(payload).action_name === "read_messages") {
      return new Promise((resolve) => {
        resolveMessages = resolve;
      });
    }
    return Promise.resolve(
      completed(
        connectorActionRequest(payload).action_name,
        responseFor(connectorActionRequest(payload).action_name, connectorActionRequest(payload).input),
      ),
    );
  });
  const { result } = renderHook(useHarness);
  await waitFor(() => expect(result.current.browser.filteredItems).toHaveLength(2));
  await act(async () => result.current.browser.selectItem(topics[0]));
  act(() => void result.current.browser.readMessages());
  await waitFor(() => expect(resolveMessages).toBeTypeOf("function"));

  await act(async () => result.current.browser.selectItem(topics[1]));
  await act(async () => resolveMessages?.(completed("read_messages", { messages: [{ offset: 42 }] })));

  expect(result.current.browser.selectedName).toBe("events");
  expect(result.current.browser.messages).toBeNull();
});

it("does not commit messages after the selected topic is cleared", async () => {
  let resolveMessages: ((_response: ConnectorActionResponse) => void) | undefined;
  vi.mocked(apiPost).mockImplementation((_path, payload) => {
    if (connectorActionRequest(payload).action_name === "read_messages") {
      return new Promise((resolve) => {
        resolveMessages = resolve;
      });
    }
    return Promise.resolve(
      completed(
        connectorActionRequest(payload).action_name,
        responseFor(connectorActionRequest(payload).action_name, connectorActionRequest(payload).input),
      ),
    );
  });
  const { result } = renderHook(useHarness);
  await waitFor(() => expect(result.current.browser.filteredItems).toHaveLength(2));
  await act(async () => result.current.browser.selectItem(topics[0]));
  act(() => void result.current.browser.readMessages());
  await waitFor(() => expect(resolveMessages).toBeTypeOf("function"));

  await act(async () => result.current.browser.selectItem(topics[0]));
  await act(async () => resolveMessages?.(completed("read_messages", { messages: [{ offset: 42 }] })));

  expect(result.current.browser.selectedName).toBe("");
  expect(result.current.browser.messages).toBeNull();
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
  vi.mocked(apiPost).mockClear();
  await act(async () => result.current.writes.publishMessage());

  expect(result.current.writes.publishDialog.error).toBe("Headers must be a JSON array.");
  expect(apiPost).not.toHaveBeenCalled();
});

function completed(actionName: string, output: unknown) {
  return connectorActionFixture({
    target_ref: "kafka:1:1",
    connector_kind: "kafka",
    action_name: actionName,
    retry_policy: { class: "read_only", guidance: "Safe to retry." },
    output,
  });
}

function responseFor(actionName: string, input: Record<string, unknown>) {
  if (actionName === "list_topics") return { topics };
  if (actionName === "describe_topic") return { name: input.topic, partitions: [{ partition: 0 }] };
  return {};
}

it("holds a Kafka publish lock until delayed activity refresh completes", async () => {
  let hold = false;
  let finishRefresh: (() => void) | undefined;
  const onRefreshActivity = vi.fn(() =>
    hold
      ? new Promise<void>((resolve) => {
          finishRefresh = resolve;
        })
      : undefined,
  );
  const { result } = renderHook(() => useHarness({ onRefreshActivity }));
  await waitFor(() => expect(result.current.browser.filteredItems).toHaveLength(2));
  await act(async () => result.current.browser.selectItem(topics[0]));
  act(() => result.current.writes.openPublishDialog());
  hold = true;
  let request: Promise<void> | undefined;
  act(() => {
    request = result.current.writes.publishMessage();
  });
  await waitFor(() => expect(finishRefresh).toBeTypeOf("function"));
  expect(result.current.browser.state.state).toBe("idle");
  expect(result.current.writes.publishPending).toBe(true);
  await act(async () => result.current.writes.publishMessage());
  expect(
    vi.mocked(apiPost).mock.calls.filter(([, payload]) => connectorActionRequest(payload).action_name === "publish_message"),
  ).toHaveLength(1);
  hold = false;
  await act(async () => {
    finishRefresh?.();
    await request;
  });
  expect(result.current.writes.publishPending).toBe(false);
  expect(result.current.writes.publishDialog.open).toBe(false);
});

it("does not close a new topic dialog or overwrite detail after an older publish", async () => {
  let finishPublish: ((_value: ConnectorActionResponse) => void) | undefined;
  vi.mocked(apiPost).mockImplementation((_path, payload) => {
    if (connectorActionRequest(payload).action_name === "publish_message")
      return new Promise((resolve) => {
        finishPublish = resolve;
      });
    return Promise.resolve(
      completed(
        connectorActionRequest(payload).action_name,
        responseFor(connectorActionRequest(payload).action_name, connectorActionRequest(payload).input),
      ),
    );
  });
  const { result } = renderHook(useHarness);
  await waitFor(() => expect(result.current.browser.filteredItems).toHaveLength(2));
  await act(async () => result.current.browser.selectItem(topics[0]));
  act(() => result.current.writes.openPublishDialog());
  let old: Promise<void> | undefined;
  act(() => {
    old = result.current.writes.publishMessage();
  });
  await act(async () => result.current.browser.selectItem(topics[1]));
  act(() => result.current.writes.openPublishDialog());
  await act(async () => {
    finishPublish?.(completed("publish_message", { published: true }));
    await old;
  });
  expect(result.current.writes.publishDialog.open).toBe(true);
  expect(result.current.browser.activeDetail?.name).toBe("events");
  expect(result.current.browser.readForm.partition).toBe("0");
});

it("dispatches exact offset strings through the shared Kafka runner", async () => {
  vi.mocked(apiPost).mockImplementation(async (_path, payload) => {
    const output =
      connectorActionRequest(payload).action_name === "list_consumer_groups"
        ? { consumer_groups: [{ name: "reader", state: "Empty" }] }
        : connectorActionRequest(payload).action_name === "describe_consumer_group"
          ? {
              name: "reader",
              partitions: [{ topic: "orders", partition: 0, committed_offset: "9007199254740993", end_offset: "9223372036854775807" }],
            }
          : responseFor(connectorActionRequest(payload).action_name, connectorActionRequest(payload).input);
    return completed(connectorActionRequest(payload).action_name, output);
  });
  const { result } = renderHook(useHarness);
  await waitFor(() => expect(result.current.browser.filteredItems).toHaveLength(2));
  await act(async () => result.current.browser.changeView("groups"));
  await act(async () => result.current.browser.selectItem(result.current.browser.filteredItems[0]));
  act(() => result.current.writes.openOffsetDialog());
  expect(result.current.writes.offsetDialog.form.offset).toBe("9007199254740993");
  await act(async () => result.current.writes.setConsumerGroupOffset());
  expect(apiPost).toHaveBeenCalledWith(
    expect.any(String),
    expect.objectContaining({
      action_name: "set_consumer_group_offset",
      input: { group: "reader", topic: "orders", partition: 0, offset: "9007199254740993" },
    }),
    expect.any(Object),
  );
  expect(result.current.writes.offsetPending).toBe(false);
  expect(result.current.writes.offsetDialog.open).toBe(false);
});

it("does not update partition controls after a pending Kafka selection is cleared", async () => {
  let finish: ((_response: ConnectorActionResponse) => void) | undefined;
  vi.mocked(apiPost).mockImplementation((_path, payload) =>
    connectorActionRequest(payload).action_name === "describe_topic"
      ? new Promise((resolve) => {
          finish = resolve;
        })
      : Promise.resolve(
          completed(
            connectorActionRequest(payload).action_name,
            responseFor(connectorActionRequest(payload).action_name, connectorActionRequest(payload).input),
          ),
        ),
  );
  const { result } = renderHook(useHarness);
  await waitFor(() => expect(result.current.browser.filteredItems).toHaveLength(2));
  let request: Promise<void> | undefined;
  act(() => {
    request = result.current.browser.selectItem(topics[0]);
  });
  await waitFor(() => expect(finish).toBeTypeOf("function"));
  await act(async () => result.current.browser.selectItem(topics[0]));
  await act(async () => {
    finish?.(completed("describe_topic", { partitions: [{ partition: 7 }] }));
    await request;
  });
  expect(result.current.browser.activeDetail).toBeNull();
  expect(result.current.browser.readForm.partition).toBe("0");
  expect(result.current.browser.state.state).toBe("idle");
});

it.each(["target", "session", "unmount"])("does not resume a Kafka write continuation after %s changes", async (change) => {
  let finish: ((_response: ConnectorActionResponse) => void) | undefined;
  vi.mocked(apiPost).mockImplementation((_path, payload) =>
    connectorActionRequest(payload).action_name === "publish_message"
      ? new Promise((resolve) => {
          finish = resolve;
        })
      : Promise.resolve(
          completed(
            connectorActionRequest(payload).action_name,
            responseFor(connectorActionRequest(payload).action_name, connectorActionRequest(payload).input),
          ),
        ),
  );
  const initialProps: Partial<KafkaBrowserProps> = {};
  const { result, rerender, unmount } = renderHook((props) => useHarness(props), { initialProps });
  await waitFor(() => expect(result.current.browser.filteredItems).toHaveLength(2));
  await act(async () => result.current.browser.selectItem(topics[0]));
  act(() => result.current.writes.openPublishDialog());
  let request: Promise<void> | undefined;
  act(() => {
    request = result.current.writes.publishMessage();
  });
  if (change === "target") rerender({ target: { ref: "kafka:2:2" } });
  else if (change === "session") rerender({ session: { active: true, startedAt: "later" } });
  else unmount();
  const detailCount = () =>
    vi.mocked(apiPost).mock.calls.filter(([, payload]) => connectorActionRequest(payload).action_name === "describe_topic").length;
  const before = detailCount();
  await act(async () => {
    finish?.(completed("publish_message", { published: true }));
    await request;
  });
  expect(detailCount()).toBe(before);
  if (change !== "unmount") {
    expect(result.current.writes.publishDialog.open).toBe(false);
    expect(result.current.writes.publishPending).toBe(false);
  }
});

it("releases the Kafka write lock after failure so the same dialog can retry", async () => {
  let fail = true;
  vi.mocked(apiPost).mockImplementation(async (_path, payload) => {
    if (connectorActionRequest(payload).action_name === "publish_message" && fail) throw new Error("publish denied");
    return completed(
      connectorActionRequest(payload).action_name,
      responseFor(connectorActionRequest(payload).action_name, connectorActionRequest(payload).input),
    );
  });
  const { result } = renderHook(useHarness);
  await waitFor(() => expect(result.current.browser.filteredItems).toHaveLength(2));
  await act(async () => result.current.browser.selectItem(topics[0]));
  act(() => result.current.writes.openPublishDialog());
  await act(async () => result.current.writes.publishMessage());
  expect(result.current.browser.state.error).toBe("publish denied");
  expect(result.current.writes.publishPending).toBe(false);
  expect(result.current.writes.publishDialog.open).toBe(true);
  fail = false;
  await act(async () => result.current.writes.publishMessage());
  expect(result.current.writes.publishDialog.open).toBe(false);
  expect(
    vi.mocked(apiPost).mock.calls.filter(([, payload]) => connectorActionRequest(payload).action_name === "publish_message"),
  ).toHaveLength(2);
});
