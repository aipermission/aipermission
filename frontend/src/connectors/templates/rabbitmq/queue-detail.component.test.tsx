import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet } from "../../../lib/api";
import type { ConnectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { QueueDetail } from "./queue-detail";
import { useRabbitMQBrowser } from "./use-rabbitmq-browser";
import type { RabbitBrowserProps } from "./browser-types";

vi.mock("../../../lib/api", () => ({ apiGet: vi.fn() }));
vi.mock("../_shared/action-runner", () => ({ runGuardedConnectorAction: vi.fn() }));

const queues = [{ name: "jobs.ready", vhost: "/", state: "running", messages: 2 }];

beforeEach(() => {
  vi.mocked(apiGet).mockReset().mockResolvedValue([]);
  vi.mocked(runGuardedConnectorAction)
    .mockReset()
    .mockImplementation(async ({ actionName, input }) => {
      const output =
        actionName === "list_queues"
          ? { queues }
          : actionName === "get_queue"
            ? queues[0]
            : actionName === "list_bindings"
              ? { bindings: [{ routing_key: "jobs.ready" }] }
              : actionName === "peek_messages"
                ? { messages: [{ payload: "job preview", payload_encoding: "string" }] }
                : { routed: true };
      const response: ConnectorActionResponse = {
        status: "completed",
        request_id: 1,
        target_ref: "rabbitmq:1:1",
        connector_kind: "rabbitmq",
        action_name: actionName,
        retry_policy: {
          class: actionName === "publish_message" ? "non_idempotent" : "read_only",
          guidance: "Inspect the result before retrying.",
        },
        output,
      };
      expect(input?.vhost).toBe("/");
      return response;
    });
});

function QueueWorkspace({ approvals = { state: "ready", data: [] } }: { approvals?: RabbitBrowserProps["approvals"] }) {
  const browser = useRabbitMQBrowser({
    target: { ref: "rabbitmq:1:1", config: { vhost: "/" } },
    approvals,
    session: { active: true, startedAt: "test-session" },
    onRefreshActivity: vi.fn(),
  });
  return (
    <>
      <button type="button" onClick={() => void browser.selectQueue("jobs.ready")}>
        Select queue
      </button>
      <QueueDetail browser={browser} styles={connectorConsoleTheme("dark")} />
    </>
  );
}

it("renders queue detail and peeks messages using the operator's bounded count", async () => {
  const user = userEvent.setup();
  render(<QueueWorkspace />);
  expect(screen.getByText("No queue selected.")).toBeVisible();
  expect(screen.getByRole("button", { name: "Peek" })).toBeDisabled();
  await waitFor(() => expect(screen.getByRole("button", { name: "Publish" })).toBeEnabled());
  await user.click(screen.getByRole("button", { name: "Select queue" }));
  expect(await screen.findByText("running")).toBeVisible();
  expect(screen.getByText(/"routing_key": "jobs.ready"/)).toBeVisible();
  const count = screen.getByRole("spinbutton", { name: "Peek count" });
  await user.clear(count);
  await user.type(count, "8");
  await user.click(screen.getByRole("button", { name: "Peek" }));
  expect(await screen.findByText(/job preview/)).toBeVisible();
  expect(runGuardedConnectorAction).toHaveBeenCalledWith(
    expect.objectContaining({
      actionName: "peek_messages",
      input: { vhost: "/", queue: "jobs.ready", count: 8, max_payload_bytes: 65536 },
    }),
  );
  const copy = screen.getByRole("button", { name: "JSON" });
  expect(copy).toBeEnabled();
  expect(copy).toHaveAttribute("title", "Copy queue JSON");
});

it("publishes edited exchange, queue routing, properties and payload through the real form", async () => {
  const user = userEvent.setup();
  render(<QueueWorkspace />);
  await waitFor(() => expect(screen.getByRole("button", { name: "Publish" })).toBeEnabled());
  await user.click(screen.getByRole("button", { name: "Publish" }));
  expect(screen.getByRole("button", { name: "Publish message" })).toBeDisabled();
  await user.click(screen.getByRole("combobox", { name: "Search queue routing keys" }));
  await user.click(screen.getByRole("option", { name: /jobs.ready/ }));
  const exchange = screen.getByRole("textbox", { name: "Publish exchange" });
  await user.clear(exchange);
  await user.type(exchange, "jobs.events");
  const properties = screen.getByRole("textbox", { name: "Publish properties JSON" });
  await user.clear(properties);
  await user.click(properties);
  await user.paste('{"content_type":"text/plain"}');
  await user.type(screen.getByRole("textbox", { name: "Publish payload" }), "New job");
  await user.click(screen.getByRole("button", { name: "Publish message" }));
  expect(runGuardedConnectorAction).toHaveBeenCalledWith(
    expect.objectContaining({
      actionName: "publish_message",
      input: {
        vhost: "/",
        exchange: "jobs.events",
        routing_key: "jobs.ready",
        payload: "New job",
        payload_encoding: "string",
        properties: { content_type: "text/plain" },
      },
    }),
  );
  await waitFor(() => expect(screen.getByRole("textbox", { name: "Publish payload" })).toHaveValue(""));
  await user.click(screen.getByRole("button", { name: "Back to detail" }));
  expect(screen.getByRole("button", { name: "Peek" })).toBeDisabled();
});

it("supports a custom routing key without overwriting the selected queue", async () => {
  const user = userEvent.setup();
  render(<QueueWorkspace />);
  await waitFor(() => expect(screen.getByRole("button", { name: "Publish" })).toBeEnabled());
  await user.click(screen.getByRole("button", { name: "Select queue" }));
  await screen.findByText("running");
  await user.click(screen.getByRole("button", { name: "Publish" }));
  expect(screen.getByText(/Publishing to jobs.ready through amq.default/)).toBeVisible();
  await user.click(screen.getByRole("combobox", { name: "Search queue routing keys" }));
  await user.click(screen.getByRole("option", { name: /Custom routing key/ }));
  const routing = screen.getByRole("textbox", { name: "Custom routing key" });
  expect(routing).toHaveValue("jobs.ready");
  await user.clear(routing);
  await user.type(routing, "jobs.custom");
  await user.type(screen.getByRole("textbox", { name: "Publish payload" }), "Custom job");
  await user.click(screen.getByRole("button", { name: "Publish message" }));
  expect(runGuardedConnectorAction).toHaveBeenCalledWith(
    expect.objectContaining({ actionName: "publish_message", input: expect.objectContaining({ routing_key: "jobs.custom" }) }),
  );
  await user.click(screen.getByRole("button", { name: "Back to detail" }));
  expect(screen.getByText("jobs.ready")).toBeVisible();
});

async function preparePublish(user: ReturnType<typeof userEvent.setup>) {
  await waitFor(() => expect(screen.getByRole("button", { name: "Publish" })).toBeEnabled());
  await user.click(screen.getByRole("button", { name: "Publish" }));
  await user.click(screen.getByRole("combobox", { name: "Search queue routing keys" }));
  await user.click(screen.getByRole("option", { name: /jobs.ready/ }));
  await user.type(screen.getByRole("textbox", { name: "Publish payload" }), "Owned payload");
}

function expectPublishLocked() {
  for (const name of ["Publish exchange", "Publish properties JSON", "Publish payload"]) {
    expect(screen.getByRole("textbox", { name })).toBeDisabled();
  }
  expect(screen.getByRole("combobox", { name: "Search queue routing keys" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Publish message" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Back to detail" })).toBeDisabled();
  expect(screen.getByRole("textbox", { name: "Publish payload" })).toHaveValue("Owned payload");
}

it("locks the real publish form until its deferred action completes", async () => {
  const user = userEvent.setup();
  const original = vi.mocked(runGuardedConnectorAction).getMockImplementation();
  if (!original) throw new Error("Missing read action fixture");
  let resolve!: (_item: ConnectorActionResponse) => void;
  const pending = new Promise<ConnectorActionResponse>((done) => {
    resolve = done;
  });
  vi.mocked(runGuardedConnectorAction).mockImplementation(async (options) => {
    if (options.actionName !== "publish_message") return original(options);
    options.setState({ state: "publishing", error: "", message: "" });
    const response = await pending;
    options.setState({ state: "idle", error: "", message: "Published" });
    return response;
  });
  render(<QueueWorkspace />);
  await preparePublish(user);
  await user.click(screen.getByRole("button", { name: "Publish message" }));
  expectPublishLocked();
  await user.click(screen.getByRole("button", { name: "Back to detail" }));
  expectPublishLocked();
  resolve({
    status: "completed",
    request_id: 71,
    target_ref: "rabbitmq:1:1",
    connector_kind: "rabbitmq",
    action_name: "publish_message",
    retry_policy: { class: "non_idempotent", guidance: "Fixture result; do not repeat the publish." },
    output: { routed: true },
  });
  await waitFor(() => expect(screen.getByRole("textbox", { name: "Publish payload" })).toHaveValue(""));
  expect(screen.getByRole("button", { name: "Back to detail" })).toBeEnabled();
});

it("keeps the form locked through approval pending until a terminal activity arrives", async () => {
  const user = userEvent.setup();
  const original = vi.mocked(runGuardedConnectorAction).getMockImplementation();
  if (!original) throw new Error("Missing read action fixture");
  vi.mocked(runGuardedConnectorAction).mockImplementation(async (options) => {
    if (options.actionName !== "publish_message") return original(options);
    options.onPending?.({
      status: "approval_pending",
      request_id: 72,
      target_ref: "rabbitmq:1:1",
      connector_kind: "rabbitmq",
      action_name: "publish_message",
      retry_policy: { class: "non_idempotent", guidance: "Fixture result; await approval." },
    });
    options.setState({ state: "idle", error: "", message: "Awaiting approval" });
    return null;
  });
  const view = render(<QueueWorkspace />);
  await preparePublish(user);
  await user.click(screen.getByRole("button", { name: "Publish message" }));
  expectPublishLocked();
  const calls = vi.mocked(runGuardedConnectorAction).mock.calls.filter(([options]) => options.actionName === "publish_message");
  await user.click(screen.getByRole("button", { name: "Publish message" }));
  expect(vi.mocked(runGuardedConnectorAction).mock.calls.filter(([options]) => options.actionName === "publish_message")).toHaveLength(
    calls.length,
  );
  const pending = { id: 72, target_ref: "rabbitmq:1:1", action_name: "publish_message", status: "approval_pending" };
  view.rerender(<QueueWorkspace approvals={{ state: "ready", data: [pending] }} />);
  expectPublishLocked();
  view.rerender(<QueueWorkspace approvals={{ state: "ready", data: [{ ...pending, status: "completed" }] }} />);
  await waitFor(() => expect(screen.getByRole("button", { name: "Back to detail" })).toBeEnabled());
  expect(screen.getByRole("textbox", { name: "Publish payload" })).toHaveValue("Owned payload");
});
