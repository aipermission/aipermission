import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { DatabaseLockDialog } from "./database-lock-dialog";

it("keeps lock choices distinct and shows failures without closing the dialog", async () => {
  const user = userEvent.setup();
  const onLock = vi.fn();
  const onClose = vi.fn();
  const { rerender } = render(<DatabaseLockDialog state={{ open: true, state: "idle", error: "" }} onClose={onClose} onLock={onLock} />);

  await user.click(screen.getByRole("button", { name: "Lock current" }));
  await user.click(screen.getByRole("button", { name: "Lock all" }));
  expect(onLock.mock.calls).toEqual([["current"], ["all"]]);

  rerender(<DatabaseLockDialog state={{ open: true, state: "locking", error: "lock failed" }} onClose={onClose} onLock={onLock} />);
  expect(screen.getByText("lock failed")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Lock current" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Lock all" })).toBeDisabled();
});
