import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost, currentWorkspaceBinding } from "../../../../lib/api";
import { RoleHistoryDialog } from "./role-history-dialog";
import { deferredRoleHistoryReply, roleHistoryCursorPageFixture } from "../../../../test/postgres/role-history-fixtures.test";

vi.mock("../../../../lib/api", () => ({ apiPost: vi.fn(), currentWorkspaceBinding: vi.fn() }));
const post = vi.mocked(apiPost);
const target = { id: 7, name: "Production" };

function response(count = 1, after = 0, more = false) {
  const page = roleHistoryCursorPageFixture(target.id, after + 1, count, more);
  for (const entry of page.entries) {
    entry.record.status = "cleanup_intent";
    entry.record.intent.role_name = `reader_${entry.resource_id}`;
  }
  return page;
}

beforeEach(() => {
  post.mockReset().mockResolvedValue(response());
  vi.mocked(currentWorkspaceBinding).mockReset().mockReturnValue("workspace-a");
});

it("shows a compact read-only identity list and refreshes without a previous snapshot", async () => {
  render(<RoleHistoryDialog target={target} onClose={vi.fn()} />);
  const list = await screen.findByRole("list", { name: "Role history" });
  const row = within(list).getByRole("listitem");
  for (const text of [
    "reader_1",
    "cleanup intent",
    "18446744073709551615",
    "Main DB (OID 12)",
    "Main Admin (OID 10)",
    "42",
    "1",
    "b".repeat(32),
  ]) {
    expect(within(row).getByText(text)).toBeInTheDocument();
  }
  expect(screen.getByRole("dialog", { name: "Production role history" })).toBeInTheDocument();
  expect(screen.getByText("No admin profile is available. Role evidence is read-only.")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /Verify role presence/ })).not.toBeInTheDocument();
  const pending = deferredRoleHistoryReply();
  post.mockReturnValueOnce(pending.promise);
  const refresh = screen.getByRole("button", { name: "Refresh role history" });
  expect(refresh).toHaveAttribute("title", "Refresh role history");
  fireEvent.click(refresh);
  expect(screen.getByText("Loading role history...")).toBeInTheDocument();
  expect(screen.queryByRole("list")).not.toBeInTheDocument();
  expect(refresh).toBeDisabled();
  await act(async () => {
    pending.resolve(response(1, 1));
  });
  expect(await screen.findByText("reader_2")).toBeInTheDocument();
  expect(screen.queryByText("reader_1")).not.toBeInTheDocument();
  for (const [path, body, options] of post.mock.calls) {
    expect(path).toBe("/api/connector-targets/7/operations/role-lifecycle-status");
    expect(body).toEqual({});
    expect(options?.signal).toBeInstanceOf(AbortSignal);
  }
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
});

it.each(["Previous", "First"])("replaces pages with Next and %s and keeps cursor requests read-only", async (navigation) => {
  post
    .mockResolvedValueOnce(response(64, 0, true))
    .mockResolvedValueOnce(response(1, 64))
    .mockResolvedValueOnce(response(64, 0, true));
  render(<RoleHistoryDialog target={target} onClose={vi.fn()} />);
  const next = screen.getByRole("button", { name: "Next" });
  const previous = screen.getByRole("button", { name: "Previous" });
  const first = screen.getByRole("button", { name: "First" });
  await waitFor(() => expect(next).toBeEnabled());
  expect(previous).toBeDisabled();
  expect(first).toBeDisabled();
  fireEvent.click(next);
  expect(await screen.findByText("reader_65")).toBeInTheDocument();
  expect(screen.queryByText("reader_1")).not.toBeInTheDocument();
  expect(screen.getByText("Page 2")).toBeInTheDocument();
  expect(first).toBeEnabled();
  expect(next).toBeDisabled();
  expect(post.mock.calls[1][1]).toEqual({ after_resource_id: "64" });
  fireEvent.click(screen.getByRole("button", { name: navigation }));
  expect(screen.queryByRole("list")).not.toBeInTheDocument();
  expect(await screen.findByText("reader_1")).toBeInTheDocument();
  expect(screen.queryByText("reader_65")).not.toBeInTheDocument();
  expect(post.mock.calls[2][1]).toEqual({});
  expect(screen.getByText("Page 1")).toBeInTheDocument();
  expect(previous).toBeDisabled();
  expect(first).toBeDisabled();
  expect(next).toBeEnabled();
  expect(screen.getAllByRole("listitem")).toHaveLength(64);
  expect(screen.queryByText(/limit reached/)).not.toBeInTheDocument();
  expect(post).toHaveBeenCalledTimes(3);
});

it("shows static empty, invalid-response, and workspace-change notices", async () => {
  post.mockResolvedValueOnce(response(0)).mockResolvedValueOnce({ password: "never-render-this" });
  const { rerender } = render(<RoleHistoryDialog target={{ id: 7 }} onClose={vi.fn()} />);
  expect(await screen.findByText("No role history.")).toBeInTheDocument();
  expect(screen.getByRole("dialog", { name: "Target 7 role history" })).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Refresh role history" }));
  expect(await screen.findByText("Unable to load Postgres role history.")).toBeInTheDocument();
  expect(screen.queryByText(/never-render/)).not.toBeInTheDocument();
  vi.mocked(currentWorkspaceBinding).mockReturnValue("workspace-b");
  rerender(<RoleHistoryDialog target={{ id: 7 }} onClose={vi.fn()} />);
  expect(screen.getByText("Workspace changed. Close this dialog and reopen role history.")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Refresh role history" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
  expect(post).toHaveBeenCalledTimes(2);
});

it.each(["Close", "Close dialog", "Escape"])("closes through %s while loading and aborts on parent unmount", async (action) => {
  const pending = deferredRoleHistoryReply();
  post.mockReturnValueOnce(pending.promise);
  const close = vi.fn();
  const { unmount } = render(<RoleHistoryDialog target={target} onClose={close} />);
  if (action === "Escape") fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
  else fireEvent.click(screen.getByRole("button", { name: action }));
  expect(close).toHaveBeenCalledTimes(1);
  const signal = post.mock.calls[0][2]!.signal!;
  unmount();
  expect(signal.aborted).toBe(true);
  await act(async () => {
    pending.resolve(response());
  });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
