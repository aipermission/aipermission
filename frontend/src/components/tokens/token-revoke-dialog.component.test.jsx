import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { TokenRevokeDialog } from "./token-revoke-dialog";

describe("TokenRevokeDialog", () => {
  it("requires an explicit irreversible revoke decision", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    const onConfirm = vi.fn();
    render(<TokenRevokeDialog token={{ name: "maintenance" }} actionState="idle" onClose={onClose} onConfirm={onConfirm} />);

    expect(screen.getByText(/cannot be undone/i)).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Revoke token" }));
    expect(onConfirm).toHaveBeenCalledOnce();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("cannot be dismissed while revocation is running", () => {
    render(<TokenRevokeDialog token={{ name: "maintenance" }} actionState="revoking" onClose={vi.fn()} onConfirm={vi.fn()} />);

    expect(screen.getByRole("button", { name: "Revoking..." })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Close dialog" })).toBeDisabled();
  });
});
