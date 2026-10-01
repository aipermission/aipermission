import { act, fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { RoleHistoryLoader } from "./role-history-loader";

const loading = vi.hoisted(() => {
  let resolve!: () => void;
  const ready = new Promise<void>((yes) => {
    resolve = yes;
  });
  return { ready, resolve, workspace: "workspace-a", post: vi.fn() };
});
vi.mock("../../../../lib/api", () => ({ apiPost: loading.post, currentWorkspaceBinding: () => loading.workspace }));
vi.mock("./role-history-dialog", async (actual) => {
  await loading.ready;
  return actual();
});

it("keeps code loading dismissible and binds delayed history to the workspace that opened it", async () => {
  const close = vi.fn();
  const dismissed = render(<RoleHistoryLoader target={{ id: 1 }} onClose={close} />);
  expect(await screen.findByRole("status")).toHaveTextContent("Loading role history...");
  fireEvent.click(screen.getByRole("button", { name: "Close dialog" }));
  expect(close).toHaveBeenCalledTimes(1);
  dismissed.unmount();
  render(<RoleHistoryLoader target={{ id: 1 }} onClose={close} />);
  loading.workspace = "workspace-b";
  await act(async () => {
    loading.resolve();
  });
  expect(await screen.findByText("Workspace changed. Close this dialog and reopen role history.")).toBeInTheDocument();
  expect(loading.post).not.toHaveBeenCalled();
});
