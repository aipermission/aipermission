import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { PendingVaultApprovalNotice } from "./pending-vault-approval-notice";

it("renders one pending approval and opens its review", async () => {
  const onReview = vi.fn();
  const user = userEvent.setup();
  render(<PendingVaultApprovalNotice count={1} onReview={onReview} />);

  expect(screen.getByText("1 pending Vault approval")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Review pending Vault approval" }));
  expect(onReview).toHaveBeenCalledOnce();
});

it("pluralizes multiple pending approvals", () => {
  render(<PendingVaultApprovalNotice count={2} onReview={() => {}} />);
  expect(screen.getByText("2 pending Vault approvals")).toBeVisible();
});
