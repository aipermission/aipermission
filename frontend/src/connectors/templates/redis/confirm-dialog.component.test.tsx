import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { RedisConfirmDialog } from "./confirm-dialog";
import type { RedisConfirmState } from "./browser-types";

function confirmation(overrides: Partial<RedisConfirmState> = {}): RedisConfirmState {
  return {
    open: true,
    type: "string",
    title: "Save value",
    description: "Confirm the selected key change.",
    details: [{ label: "Key", value: "example:key" }],
    tone: "warn",
    pending: false,
    error: "",
    onConfirm: null,
    ...overrides,
  };
}

it("shows write details and forwards confirmation and cancellation", () => {
  const onClose = vi.fn();
  const onConfirm = vi.fn();
  render(<RedisConfirmDialog value={confirmation()} theme="light" product="Valkey" onClose={onClose} onConfirm={onConfirm} />);
  expect(screen.getByText("Review the Valkey write before continuing.")).toBeVisible();
  expect(screen.getByText("example:key")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
  expect(onConfirm).toHaveBeenCalledOnce();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(onClose).toHaveBeenCalledOnce();
});

it("keeps failed destructive confirmations open with their error", () => {
  render(
    <RedisConfirmDialog
      value={confirmation({ tone: "bad", error: "Write rejected", details: [] })}
      theme="dark"
      product="Redis"
      onClose={vi.fn()}
      onConfirm={vi.fn()}
    />,
  );
  expect(screen.getByText("This operation cannot be undone.")).toBeVisible();
  expect(screen.getByText("Write rejected")).toBeVisible();
  expect(screen.getByRole("button", { name: "Delete" })).toBeEnabled();
  expect(screen.queryByText("example:key")).not.toBeInTheDocument();
});

it("prevents duplicate submission and dismissal while a write is pending", () => {
  const onClose = vi.fn();
  const onConfirm = vi.fn();
  render(
    <RedisConfirmDialog value={confirmation({ pending: true })} theme="dark" product="Redis" onClose={onClose} onConfirm={onConfirm} />,
  );
  expect(screen.getByRole("button", { name: "Working..." })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Working..." }));
  fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
  expect(onConfirm).not.toHaveBeenCalled();
  expect(onClose).not.toHaveBeenCalled();
});

it("does not mount closed confirmations", () => {
  render(<RedisConfirmDialog value={confirmation({ open: false })} theme="light" product="Redis" onClose={vi.fn()} onConfirm={vi.fn()} />);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
