import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { defaultS3ConfirmDialog, S3ConfirmDialog } from "./dialogs";

it("shows destructive S3 confirmation state and blocks a second submit while pending", async () => {
  const user = userEvent.setup();
  const onConfirm = vi.fn();
  const onClose = vi.fn();
  const value = {
    ...defaultS3ConfirmDialog,
    open: true,
    title: "Delete object",
    description: "Review the object key.",
    details: [{ label: "Object key", value: "live/file.txt" }],
    danger: true,
    status: "Awaiting approval",
    error: "Previous request failed",
  };
  const { rerender } = render(<S3ConfirmDialog value={value} theme="dark" onClose={onClose} onConfirm={onConfirm} />);

  expect(screen.getByText("live/file.txt")).toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveTextContent("Awaiting approval");
  expect(screen.getByRole("alert")).toHaveTextContent("Previous request failed");
  await user.click(screen.getByRole("button", { name: "Delete" }));
  expect(onConfirm).toHaveBeenCalledOnce();

  rerender(<S3ConfirmDialog value={{ ...value, pending: true }} theme="dark" onClose={onClose} onConfirm={onConfirm} />);
  expect(screen.getByRole("button", { name: "Working..." })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
});
