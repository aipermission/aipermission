import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../../lib/api";
import { connectorActionRequest } from "../../../test/connector-action-fixtures";
import { useRabbitMQBrowser } from "./use-rabbitmq-browser";
import type { ConnectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import type { RabbitBrowserProps } from "./browser-types";

vi.mock("../../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn(), currentWorkspaceBinding: () => "peek-workspace" }));

function completed(actionName: string, output: unknown): ConnectorActionResponse {
  return {
    request_id: 1,
    status: "completed",
    target_ref: "rabbitmq:1:1",
    connector_kind: "rabbitmq",
    action_name: actionName,
    retry_policy: { class: "read_only", guidance: "Read again." },
    output,
  };
}

function deferred() {
  let resolve!: (_value: unknown) => void;
  let reject!: (_error: Error) => void;
  const promise = new Promise<unknown>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

function renderBrowser() {
  const props: RabbitBrowserProps = {
    target: { ref: "rabbitmq:1:1" },
    session: { active: true, startedAt: "session-a" },
    approvals: { state: "ready", data: [] },
    onRefreshActivity: vi.fn(),
  };
  return { ...renderHook((next) => useRabbitMQBrowser(next), { initialProps: props }), props };
}

beforeEach(() => {
  vi.mocked(apiGet).mockReset().mockResolvedValue([]);
  vi.mocked(apiPost)
    .mockReset()
    .mockImplementation(async (_path, payload) => {
      const action = connectorActionRequest(payload);
      const outputs: Record<string, unknown> = {
        list_queues: { queues: [{ name: "jobs.ready" }] },
        get_queue: { name: action.input.queue },
        list_bindings: { bindings: [] },
        peek_messages: { messages: [{ payload: "Current message" }] },
      };
      return completed(action.action_name, outputs[action.action_name]);
    });
});

describe("RabbitMQ peek session ownership", () => {
  it("does not dispatch a peek after closing the session without changing its timestamp", async () => {
    const hook = renderBrowser();
    await act(async () => {
      await hook.result.current.selectQueue("jobs.ready");
    });
    expect(hook.result.current.activeQueue).toBe("jobs.ready");
    hook.rerender({ ...hook.props, session: { active: false, startedAt: "session-a" } });
    const calls = vi.mocked(apiPost).mock.calls.length;
    await act(async () => {
      await hook.result.current.peekMessages();
    });
    expect(apiPost).toHaveBeenCalledTimes(calls);
  });

  it.each(["success", "failure"])("retires an in-flight peek %s when closing and reopening the same session identity", async (outcome) => {
    const old = deferred();
    const hook = renderBrowser();
    await act(async () => {
      await hook.result.current.selectQueue("jobs.ready");
    });
    vi.mocked(apiPost).mockImplementationOnce(() => old.promise);
    let peeking!: Promise<void>;
    act(() => {
      peeking = hook.result.current.peekMessages();
    });
    const signal = vi.mocked(apiPost).mock.calls.at(-1)?.[2]?.signal;
    hook.rerender({ ...hook.props, session: { active: false, startedAt: "session-a" } });
    expect(signal?.aborted).toBe(true);
    hook.rerender(hook.props);
    await act(async () => {
      await hook.result.current.selectQueue("jobs.ready");
    });
    await act(async () => {
      await hook.result.current.peekMessages();
    });
    await waitFor(() => expect(hook.result.current.messages).toEqual([{ payload: "Current message" }]));
    await act(async () => {
      if (outcome === "success") old.resolve(completed("peek_messages", { messages: [{ payload: "Old message" }] }));
      else old.reject(new Error("Old peek failed"));
      await peeking;
    });
    expect(hook.result.current.messages).toEqual([{ payload: "Current message" }]);
    expect(hook.result.current.state.error).toBe("");
  });

  it("rejects a retained peek handler after queue replacement and unmount", async () => {
    const hook = renderBrowser();
    await act(async () => {
      await hook.result.current.selectQueue("jobs.ready");
    });
    const older = hook.result.current.peekMessages;
    await act(async () => {
      await hook.result.current.selectQueue("jobs.other");
    });
    const calls = vi.mocked(apiPost).mock.calls.length;
    await act(async () => {
      await older();
    });
    expect(apiPost).toHaveBeenCalledTimes(calls);
    const current = hook.result.current.peekMessages;
    hook.unmount();
    await current();
    expect(apiPost).toHaveBeenCalledTimes(calls);
  });
});
