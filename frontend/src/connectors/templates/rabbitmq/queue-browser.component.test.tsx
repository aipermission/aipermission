import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { QueueBrowser } from "./queue-browser";
import { QueueDetail } from "./queue-detail";
import { queueNameLabel } from "./helpers";
import { connectorConsoleTheme } from "../_shared/console-theme";
import type { RabbitBrowser } from "./use-rabbitmq-browser";

const styles = connectorConsoleTheme("dark");

function browser(overrides: Partial<RabbitBrowser> = {}): RabbitBrowser {
  return {
    activeSession: { active: true },
    bindings: [],
    messages: [],
    queueDetail: null,
    peekCount: 5,
    setPeekCount: vi.fn(),
    detailMode: "inspect",
    setDetailMode: vi.fn(),
    publish: { exchange: "amq.default", customRoutingKey: false, routingKey: "", payload: "", properties: "{}" },
    setPublish: vi.fn(),
    startPublish: vi.fn(),
    publishMessage: vi.fn(async () => {}),
    peekMessages: vi.fn(async () => {}),
    activeQueue: "",
    applyVhost: vi.fn(),
    filteredQueues: [],
    latestAction: null,
    pattern: "",
    publishLocked: false,
    queues: [],
    refreshQueues: vi.fn(async () => {}),
    selectQueue: vi.fn(async () => {}),
    setPattern: vi.fn(),
    setVhostDraft: vi.fn(),
    state: { state: "idle", error: "", message: "" },
    vhost: "/",
    vhostDraft: "/",
    ...overrides,
  };
}

it("edits a vhost draft and applies it only when the form is submitted", async () => {
  const user = userEvent.setup();
  const model = browser();
  render(<QueueBrowser browser={model} styles={styles} />);

  await user.type(screen.getByPlaceholderText("vhost"), "tenant");
  expect(model.setVhostDraft).toHaveBeenCalled();
  expect(model.applyVhost).not.toHaveBeenCalled();

  fireEvent.submit(screen.getByRole("button", { name: "Refresh" }).closest("form")!);
  expect(model.applyVhost).toHaveBeenCalledOnce();
});

it("locks vhost changes while publishing", () => {
  const model = browser({ publishLocked: true });
  render(<QueueBrowser browser={model} styles={styles} />);

  expect(screen.getByPlaceholderText("vhost")).toBeDisabled();
  expect(screen.getByRole("button", { name: "Refresh" })).toBeDisabled();
});

it.each([" tenant ", " \t ", ""])("forwards the exact vhost draft from the browser form: %j", (vhostDraft) => {
  const model = browser({ vhostDraft });
  render(<QueueBrowser browser={model} styles={styles} />);
  const input = screen.getByPlaceholderText("vhost");
  expect(input).toHaveValue(vhostDraft);
  const next = `${vhostDraft} `;
  fireEvent.change(input, { target: { value: next } });
  expect(model.setVhostDraft).toHaveBeenCalledExactlyOnceWith(next);
  expect(model.applyVhost).not.toHaveBeenCalled();
  fireEvent.submit(screen.getByRole("button", { name: "Refresh" }).closest("form")!);
  expect(model.applyVhost).toHaveBeenCalledOnce();
});

it("keeps queue filtering separate from the vhost form", async () => {
  const user = userEvent.setup();
  const model = browser();
  render(<QueueBrowser browser={model} styles={styles} />);

  await user.type(screen.getByPlaceholderText("Filter queues"), "jobs{Enter}");

  expect(model.setPattern).toHaveBeenCalled();
  expect(model.applyVhost).not.toHaveBeenCalled();
});

it("renders publish and inspection details through the RabbitMQ workspace", async () => {
  const user = userEvent.setup();
  const model = browser({
    activeQueue: "jobs.ready",
    bindings: [],
    detailMode: "publish",
    messages: [],
    peekCount: 5,
    peekMessages: vi.fn(async () => {}),
    publish: {
      exchange: "amq.default",
      customRoutingKey: false,
      routingKey: "jobs.ready",
      payload: "fixture",
      properties: '{"content_type":"application/json"}',
    },
    publishMessage: vi.fn(async () => {}),
    queueDetail: { name: "jobs.ready", state: "running" },
    setDetailMode: vi.fn(),
    setPeekCount: vi.fn(),
    setPublish: vi.fn(),
    startPublish: vi.fn(),
  });
  const view = render(<QueueDetail browser={model} styles={styles} />);

  expect(screen.getByRole("button", { name: "Publish message" })).toBeVisible();
  await user.click(screen.getByRole("combobox", { name: "Search queue routing keys" }));
  expect(screen.getByRole("option", { name: /Custom routing key/ })).toBeVisible();
  fireEvent.submit(screen.getByRole("button", { name: "Publish message" }).closest("form")!);
  expect(model.publishMessage).toHaveBeenCalledOnce();

  view.rerender(<QueueDetail browser={{ ...model, detailMode: "inspect" }} styles={styles} />);
  expect(screen.getByText("Queue and bindings")).toBeVisible();
  expect(screen.getByText("No messages peeked in this session.")).toBeVisible();
});

it("selects a queue while normalizing absent RabbitMQ counters", async () => {
  const user = userEvent.setup();
  const queue = { name: "jobs", vhost: "/", messages_ready: null, messages_unacknowledged: undefined, consumers: "" };
  const model = browser({ filteredQueues: [queue], queues: [queue], latestAction: { status: "failed", action_name: "list_queues" } });
  render(<QueueBrowser browser={model} styles={styles} />);
  expect(screen.getAllByText(/ready 0 · unacked 0 · consumers 0/)).toHaveLength(2);
  await user.click(screen.getByRole("button", { name: /jobs/ }));
  expect(model.selectQueue).toHaveBeenCalledWith("jobs");
});

it("distinguishes padded, whitespace-only and quoted queue names without changing selection", async () => {
  const user = userEvent.setup();
  const names = ["jobs", " jobs ", " ", "  ", "\u00a0", "\t", '" jobs "', "\\u0020"];
  const queues = names.map((name) => ({ name, vhost: "/" }));
  const model = browser({ filteredQueues: queues, queues });
  render(<QueueBrowser browser={model} styles={styles} />);
  for (const name of names) {
    const label = queueNameLabel(name);
    const button = screen.getByRole("button", { name: label });
    expect(button).toHaveTextContent(label);
    await user.click(button);
    expect(model.selectQueue).toHaveBeenLastCalledWith(name);
  }
});
