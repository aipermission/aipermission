import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDownload, apiPostForm } from "../../../lib/api";
import { BackupRestoreDialog } from "./backup-restore-dialog";

vi.mock("../../../lib/api", () => ({ apiDownload: vi.fn(), apiPostForm: vi.fn(), currentWorkspaceBinding: () => "test-workspace" }));
vi.mock("../../../lib/local-action-retry", () => ({
  prepareLocalActionRetry: vi.fn(async () => ({ idempotencyKey: "test-restore" })),
  completeLocalActionRetry: vi.fn(async () => {}),
  markLocalActionRetryOutcome: vi.fn(async () => {}),
  preserveLocalActionRetryAttempt: vi.fn(async () => {}),
  releaseLocalActionRetryAttempt: vi.fn(async () => {}),
}));

const value = { open: true, target: { id: 1, name: "Test database" }, profile: { id: 2 } };

beforeEach(() => {
  vi.mocked(apiDownload).mockReset().mockResolvedValue({ saved: true, method: "picker" });
  vi.mocked(apiPostForm).mockReset().mockResolvedValue({ operation_id: 1, status: "completed", result: {} });
});

it("defaults to backup and isolates backup errors from the restore tab", async () => {
  const user = userEvent.setup();
  vi.mocked(apiDownload).mockRejectedValueOnce(new Error("Download failed"));
  render(<BackupRestoreDialog value={value} onClose={vi.fn()} />);
  expect(screen.getByRole("tab", { name: "Backup" })).toHaveAttribute("aria-selected", "true");
  expect(screen.getByLabelText("SQL dump file")).not.toBeVisible();
  await user.click(screen.getByRole("button", { name: "Download SQL dump" }));
  expect(await screen.findByText("Download failed")).toBeVisible();
  await user.click(screen.getByRole("tab", { name: "Restore" }));
  expect(screen.queryByText("Download failed")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Restore SQL dump" })).toBeDisabled();
  await user.click(screen.getByRole("tab", { name: "Backup" }));
  expect(screen.getByText("Download failed")).toBeVisible();
});

it("enables restore only for an uploaded dump and exact target confirmation", async () => {
  const user = userEvent.setup();
  render(<BackupRestoreDialog value={value} onClose={vi.fn()} />);
  await user.click(screen.getByRole("tab", { name: "Restore" }));
  await user.upload(screen.getByLabelText("SQL dump file"), new File(["SELECT 1;"], "backup.sql", { type: "application/sql" }));
  const confirmation = screen.getByLabelText("Type target name to confirm: Test database");
  await user.type(confirmation, "test database");
  expect(screen.getByRole("button", { name: "Restore SQL dump" })).toBeDisabled();
  await user.clear(confirmation);
  await user.type(confirmation, "Test database");
  await user.click(screen.getByRole("button", { name: "Restore SQL dump" }));
  expect(await screen.findByText("Restore completed.")).toBeVisible();
  await waitFor(() => expect(screen.getByRole("button", { name: "Restore SQL dump" })).toBeDisabled());
  expect(confirmation).toHaveValue("");
  expect(screen.getByLabelText("SQL dump file")).toHaveValue("");
  await user.upload(screen.getByLabelText("SQL dump file"), new File(["SELECT 1;"], "backup.sql", { type: "application/sql" }));
  await user.type(confirmation, "Test database");
  expect(screen.getByRole("button", { name: "Restore SQL dump" })).toBeEnabled();
});

it("preserves the visible selected file when switching tabs", async () => {
  const user = userEvent.setup();
  render(<BackupRestoreDialog value={value} onClose={vi.fn()} />);
  await user.click(screen.getByRole("tab", { name: "Restore" }));
  const input = screen.getByLabelText<HTMLInputElement>("SQL dump file");
  const file = new File(["SELECT 1;"], "selected.sql", { type: "application/sql" });
  await user.upload(input, file);
  await user.type(screen.getByLabelText("Type target name to confirm: Test database"), "Test database");
  await user.click(screen.getByRole("tab", { name: "Backup" }));
  await user.click(screen.getByRole("tab", { name: "Restore" }));
  expect(input.files?.[0]).toBe(file);
  expect(screen.getByRole("button", { name: "Restore SQL dump" })).toBeEnabled();
});
