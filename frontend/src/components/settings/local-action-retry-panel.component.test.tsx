import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import {
  listLocalActionRetryEntries,
  localActionRetryLedgerChangedEvent,
  resetLocalActionRetryLedger,
  resolveLocalActionRetryEntry,
} from "../../lib/local-action-retry";
import type { RetryListEntry } from "../../lib/local-action-retry.ts";
import { LocalActionRetryPanel } from "./local-action-retry-panel";
import type { RetryEntry } from "../../lib/local-action-retry/records.ts";

vi.mock("../../lib/local-action-retry", () => ({
  listLocalActionRetryEntries: vi.fn(),
  localActionRetryLedgerChangedEvent: "aipermission:test-ledger-changed",
  resetLocalActionRetryLedger: vi.fn(),
  resolveLocalActionRetryEntry: vi.fn(),
}));

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(listLocalActionRetryEntries).mockReset();
  vi.mocked(resetLocalActionRetryLedger).mockResolvedValue(undefined);
  vi.mocked(resolveLocalActionRetryEntry).mockResolvedValue(true);
});

it("renders a request reference without trusting arbitrary persisted metadata", async () => {
  const base: RetryEntry = {
    id: "one",
    scope: "one",
    signature: "a".repeat(64),
    key: "one",
    revision: 1,
    created_at: "invalid",
    state: "outcome_unknown",
    updated_at: "invalid",
  };
  vi.mocked(listLocalActionRetryEntries).mockResolvedValue([
    { ...base, request_id: 42 },
    { ...base, signature: "b".repeat(64), request_id: {} } as unknown as RetryEntry,
  ]);
  render(<LocalActionRetryPanel />);
  expect(await screen.findByText(/Request 42/)).toBeVisible();
  expect(screen.getAllByText("Outcome unknown")).toHaveLength(2);
  expect(screen.queryByText(/object Object/)).not.toBeInTheDocument();
});

it("ignores an older ledger response after a refresh has completed", async () => {
  let finish: ((_entries: RetryListEntry[]) => void) | undefined;
  vi.mocked(listLocalActionRetryEntries)
    .mockReturnValueOnce(
      new Promise<RetryListEntry[]>((resolve) => {
        finish = resolve;
      }),
    )
    .mockResolvedValueOnce([]);
  render(<LocalActionRetryPanel />);
  await act(async () => {
    window.dispatchEvent(new Event(localActionRetryLedgerChangedEvent));
  });
  expect(await screen.findByText("No unresolved local connector attempts.")).toBeVisible();
  if (!finish) throw new Error("Initial retry ledger load did not start");
  const complete = finish;
  await act(async () =>
    complete([
      {
        signature: "legacy-v2-ledger",
        key: "",
        state: "outcome_unknown",
        created_at: "",
        updated_at: "",
        assistant_hint: "",
        invalid: true,
      },
    ]),
  );
  expect(screen.queryByText("Outcome unknown")).not.toBeInTheDocument();
});

it("requires confirmation before resetting a ledger that cannot be loaded", async () => {
  const user = userEvent.setup();
  vi.mocked(listLocalActionRetryEntries).mockRejectedValue(new Error("ledger unavailable"));
  render(<LocalActionRetryPanel />);
  expect(await screen.findByText("ledger unavailable")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Reset ledger" }));
  expect(screen.getByText(/malformed local retry state/i)).toBeVisible();
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Reset ledger" }));
  expect(resetLocalActionRetryLedger).toHaveBeenCalledOnce();
});

it("fails closed when the selected retry identity changed in another tab", async () => {
  const user = userEvent.setup();
  const entry: RetryEntry = {
    id: "one",
    scope: "one",
    signature: "a".repeat(64),
    key: "one",
    revision: 1,
    created_at: "invalid",
    state: "pending",
    operation_ref: "restore:manual",
    updated_at: "invalid",
  };
  vi.mocked(listLocalActionRetryEntries).mockResolvedValue([entry]);
  vi.mocked(resolveLocalActionRetryEntry).mockResolvedValue(false);
  render(<LocalActionRetryPanel />);
  expect(await screen.findByText(/Request acknowledgement pending/)).toBeVisible();
  expect(screen.getByText(/Time unavailable/)).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Mark retry identity reconciled" }));
  await user.click(screen.getByRole("button", { name: "Mark reconciled" }));
  expect(await screen.findByText(/changed in another tab/i)).toBeVisible();
});
