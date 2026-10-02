import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost, currentWorkspaceBinding } from "../../../../lib/api";
import { RoleHistoryDialog } from "./role-history-dialog";
import { deferredRoleHistoryReply, roleHistoryPageFixture } from "../../../../test/postgres/role-history-fixtures.test";

vi.mock("../../../../lib/api", () => ({ apiPost: vi.fn(), currentWorkspaceBinding: vi.fn() }));
const post = vi.mocked(apiPost);
const target = { id: 1, name: "Database", profiles: [{ id: 2, kind: "username_password", label: "Admin" }] };
function page() {
  const value = roleHistoryPageFixture();
  value.entries[0]!.record.status = "cleanup_intent";
  return value;
}
beforeEach(() => {
  post.mockReset().mockResolvedValue(page());
  vi.mocked(currentWorkspaceBinding).mockReset().mockReturnValue("workspace-a");
});

it.each(["Close", "Close dialog", "Escape"])("freezes %s while verifying and releases it after the evidence reload", async (action) => {
  const pending = deferredRoleHistoryReply();
  const close = vi.fn();
  render(<RoleHistoryDialog target={target} onClose={close} />);
  fireEvent.click(await screen.findByRole("button", { name: /Verify role presence/ }));
  fireEvent.click(screen.getByRole("checkbox"));
  post.mockReturnValueOnce(pending.promise).mockResolvedValueOnce(page());
  fireEvent.click(screen.getByRole("button", { name: "Confirm role presence" }));
  expect(screen.getByText("Verifying role decision...")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Refresh role history" })).toBeDisabled();
  if (action === "Escape") fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
  else fireEvent.click(screen.getByRole("button", { name: action }));
  expect(close).not.toHaveBeenCalled();
  await act(async () => {
    pending.resolve({});
  });
  await waitFor(() => expect(screen.getByRole("button", { name: "Close" })).toBeEnabled());
  expect(screen.getByText(/^Decision outcome was not confirmed/)).toBeInTheDocument();
  if (action === "Escape") fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
  else fireEvent.click(screen.getByRole("button", { name: action }));
  expect(close).toHaveBeenCalledTimes(1);
  expect(post).toHaveBeenCalledTimes(3);
});

it("releases dismissal on workspace drift without accepting the pending decision", async () => {
  const pending = deferredRoleHistoryReply();
  const close = vi.fn();
  const { rerender } = render(<RoleHistoryDialog target={target} onClose={close} />);
  fireEvent.click(await screen.findByRole("button", { name: /Verify role presence/ }));
  fireEvent.click(screen.getByRole("checkbox"));
  post.mockReturnValueOnce(pending.promise);
  fireEvent.click(screen.getByRole("button", { name: "Confirm role presence" }));
  vi.mocked(currentWorkspaceBinding).mockReturnValue("workspace-b");
  rerender(<RoleHistoryDialog target={target} onClose={close} />);
  expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();
  expect(screen.queryByRole("list")).not.toBeInTheDocument();
  await act(async () => {
    pending.resolve({});
  });
  expect(post).toHaveBeenCalledTimes(2);
  expect(screen.queryByText(/^Decision outcome/)).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(close).toHaveBeenCalledTimes(1);
});
