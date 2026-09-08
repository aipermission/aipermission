import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { QueueBrowser } from "./queue-browser";
import { QueueDetail } from "./queue-detail";

const styles = {
  activeRow: "active",
  border: "border",
  input: "input",
  muted: "muted",
  rowHover: "hover",
  subtlePanel: "panel",
};

function browser(overrides = {}) {
  return {
    activeQueue: "",
    applyVhost: vi.fn(),
    filteredQueues: [],
    latestAction: null,
    pattern: "",
    publishLocked: false,
    queues: [],
    refreshQueues: vi.fn(),
    selectQueue: vi.fn(),
    setPattern: vi.fn(),
    setVhostDraft: vi.fn(),
    state: { state: "idle" },
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

  fireEvent.submit(screen.getByPlaceholderText("vhost").closest("form"));
  expect(model.applyVhost).toHaveBeenCalledOnce();
});

it("locks vhost changes while publishing", () => {
  const model = browser({ publishLocked: true, state: { state: "idle" } });
  render(<QueueBrowser browser={model} styles={styles} />);

  expect(screen.getByPlaceholderText("vhost")).toBeDisabled();
  expect(screen.getByRole("button", { name: "Refresh" })).toBeDisabled();
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
    peekMessages: vi.fn(),
    publish: {
      exchange: "amq.default",
      customRoutingKey: false,
      routingKey: "jobs.ready",
      payload: "fixture",
      properties: '{"content_type":"application/json"}',
    },
    publishMessage: vi.fn(),
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
  fireEvent.submit(screen.getByRole("button", { name: "Publish message" }).closest("form"));
  expect(model.publishMessage).toHaveBeenCalledOnce();

  view.rerender(<QueueDetail browser={{ ...model, detailMode: "inspect" }} styles={styles} />);
  expect(screen.getByText("Queue and bindings")).toBeVisible();
  expect(screen.getByText("No messages peeked in this session.")).toBeVisible();
});
