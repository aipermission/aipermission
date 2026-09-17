import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { listLocalActionRetryEntries, resetLocalActionRetryLedger, resolveLocalActionRetryEntry } from "../../lib/local-action-retry";
import { LocalActionRetryPanel } from "./local-action-retry-panel";

vi.mock("../../lib/local-action-retry", () => ({
  listLocalActionRetryEntries: vi.fn(),
  localActionRetryLedgerChangedEvent: "aipermission:test-ledger-changed",
  resetLocalActionRetryLedger: vi.fn(),
  resolveLocalActionRetryEntry: vi.fn(),
}));

beforeEach(() => {
  vi.clearAllMocks();
  resetLocalActionRetryLedger.mockResolvedValue(undefined);
  resolveLocalActionRetryEntry.mockResolvedValue(true);
});

it("requires confirmation before resetting a ledger that cannot be loaded", async () => {
  const user = userEvent.setup();
  listLocalActionRetryEntries.mockRejectedValue(new Error("ledger unavailable"));
  render(<LocalActionRetryPanel />);
  expect(await screen.findByText("ledger unavailable")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Reset ledger" }));
  expect(screen.getByText(/malformed local retry state/i)).toBeVisible();
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Reset ledger" }));
  expect(resetLocalActionRetryLedger).toHaveBeenCalledOnce();
});

it("fails closed when the selected retry identity changed in another tab", async () => {
  const user = userEvent.setup();
  const entry = { signature: "sig", state: "pending", operation_ref: "restore:manual", updated_at: "invalid" };
  listLocalActionRetryEntries.mockResolvedValue([entry]);
  resolveLocalActionRetryEntry.mockResolvedValue(false);
  render(<LocalActionRetryPanel />);
  expect(await screen.findByText(/Request acknowledgement pending/)).toBeVisible();
  expect(screen.getByText(/Time unavailable/)).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Mark retry identity reconciled" }));
  await user.click(screen.getByRole("button", { name: "Mark reconciled" }));
  expect(await screen.findByText(/changed in another tab/i)).toBeVisible();
});
