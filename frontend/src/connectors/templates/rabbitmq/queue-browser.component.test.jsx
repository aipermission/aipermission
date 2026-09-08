import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { QueueBrowser } from "./queue-browser";

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
  const model = browser({ state: { state: "publishing" } });
  render(<QueueBrowser browser={model} styles={styles} />);

  expect(screen.getByPlaceholderText("vhost")).toBeDisabled();
  expect(screen.getByRole("button", { name: "Refresh" })).toBeDisabled();
});
