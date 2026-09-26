import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { defaultCredentialDeleteDialog, DeleteCredentialDialog } from "./credential-delete-dialog";

const idle = { state: "idle", error: null, message: null };
const row = { id: 2, name: "Reader", connector_label: "Fixture", target_label: "Database" };
const value = { open: true, row, dialog: defaultCredentialDeleteDialog(row), attempted: false };

it("describes local deletion and delegates confirmation or cancellation without consuming native row fields", async () => {
  const user = userEvent.setup();
  const onClose = vi.fn();
  const onDelete = vi.fn();
  render(<DeleteCredentialDialog value={value} state={idle} onClose={onClose} onDelete={onDelete} />);
  expect(screen.getByRole("dialog", { name: "Delete Reader" })).toBeVisible();
  expect(screen.getByText("Database")).toBeVisible();
  expect(screen.getByText(/locally stored credential profile/)).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Delete credential" }));
  expect(onDelete).toHaveBeenCalledOnce();
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(onClose).toHaveBeenCalledOnce();
});

it("shows only errors from an attempted delete and blocks dismissal during deletion", async () => {
  const user = userEvent.setup();
  const onClose = vi.fn();
  const onDelete = vi.fn();
  const props = { value, state: { ...idle, state: "error", error: "Delete failed" }, onClose, onDelete };
  const view = render(<DeleteCredentialDialog {...props} />);
  expect(screen.queryByText("Delete failed")).not.toBeInTheDocument();
  view.rerender(<DeleteCredentialDialog {...props} value={{ ...value, attempted: true }} />);
  expect(screen.getByText("Delete failed")).toBeVisible();
  view.rerender(<DeleteCredentialDialog {...props} state={{ ...idle, state: "deleting" }} />);
  expect(screen.getByRole("button", { name: "Deleting..." })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
  await user.keyboard("{Escape}");
  expect(onClose).not.toHaveBeenCalled();
  expect(onDelete).not.toHaveBeenCalled();
});

it("uses safe fallback text when a connector supplies no deletion metadata or row", () => {
  const props = { state: idle, onClose: vi.fn(), onDelete: vi.fn() };
  const view = render(<DeleteCredentialDialog {...props} value={{ ...value, dialog: null }} />);
  expect(screen.getByRole("dialog", { name: "Delete credential" })).toBeVisible();
  expect(screen.getByRole("button", { name: "Delete credential" })).toBeEnabled();
  view.rerender(
    <DeleteCredentialDialog {...props} value={{ ...value, row: null, dialog: { details: [{ label: "Empty", value: "" }] } }} />,
  );
  expect(screen.queryByRole("button", { name: "Delete credential" })).not.toBeInTheDocument();
});
