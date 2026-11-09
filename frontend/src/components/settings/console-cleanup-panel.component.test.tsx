import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPut, currentWorkspaceBinding } from "../../lib/api";
import { invalidateUISession } from "../../lib/ui-session-events";
import { ConsoleCleanupPanel } from "./console-cleanup-panel";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPut: vi.fn(), currentWorkspaceBinding: vi.fn() }));
beforeEach(() => {
  vi.mocked(apiGet).mockReset();
  vi.mocked(apiPut).mockReset();
  vi.mocked(currentWorkspaceBinding).mockReset().mockReturnValue("workspace");
});
const policy = { enabled: false, idle_minutes: 60 };

it("loads disabled defaults and saves only the selected opt-in policy", async () => {
  const user = userEvent.setup();
  vi.mocked(apiGet).mockResolvedValue(policy);
  vi.mocked(apiPut).mockResolvedValue({ enabled: true, idle_minutes: 30 });
  render(<ConsoleCleanupPanel />);
  const checkbox = await screen.findByRole("checkbox", { name: "Automatic idle cleanup" });
  await waitFor(() => expect(checkbox).toBeEnabled());
  expect(checkbox).not.toBeChecked();
  expect(screen.getByLabelText("Idle threshold")).toBeDisabled();
  await user.click(checkbox);
  expect(screen.getByText(/detached remote work may be affected/)).toBeVisible();
  expect(screen.getByLabelText("Idle threshold").querySelectorAll("option")).toHaveLength(8);
  await user.selectOptions(screen.getByLabelText("Idle threshold"), "30");
  expect(apiPut).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Save console cleanup" }));
  expect(apiPut).toHaveBeenCalledWith(
    "/api/settings/console-cleanup",
    { enabled: true, idle_minutes: 30 },
    { signal: expect.any(AbortSignal), timeoutMs: 4000, workspaceBinding: "workspace" },
  );
  expect(await screen.findByText(/Next check is at the start of the next hour/)).toBeVisible();
});

it("disables automatic cleanup and retains the chosen threshold", async () => {
  const user = userEvent.setup();
  vi.mocked(apiGet).mockResolvedValue({ enabled: true, idle_minutes: 120 });
  vi.mocked(apiPut).mockResolvedValue({ enabled: false, idle_minutes: 120 });
  render(<ConsoleCleanupPanel />);
  await waitFor(() => expect(screen.getByRole("checkbox")).toBeChecked());
  await user.click(screen.getByRole("checkbox"));
  await user.click(screen.getByRole("button", { name: "Save console cleanup" }));
  expect(await screen.findByText("Automatic console cleanup disabled.")).toBeVisible();
  expect(screen.getByLabelText("Idle threshold")).toHaveValue("120");
});

it.each([null, {}, { enabled: true, idle_minutes: 0 }, { enabled: "yes", idle_minutes: 30 }])(
  "rejects malformed settings without enabling mutation: %j",
  async (value) => {
    vi.mocked(apiGet).mockResolvedValue(value);
    render(<ConsoleCleanupPanel />);
    expect(await screen.findByText("Invalid console cleanup settings.")).toBeVisible();
    expect(screen.getByRole("button", { name: "Save console cleanup" })).toBeDisabled();
    expect(apiPut).not.toHaveBeenCalled();
  },
);

it("requires a fresh read after an unknown save outcome", async () => {
  const user = userEvent.setup();
  vi.mocked(apiGet).mockResolvedValue(policy);
  vi.mocked(apiPut).mockRejectedValue(new Error("Reply lost"));
  render(<ConsoleCleanupPanel />);
  await waitFor(() => expect(screen.getByRole("button", { name: "Save console cleanup" })).toBeEnabled());
  await user.click(screen.getByRole("button", { name: "Save console cleanup" }));
  expect(await screen.findByText(/the update may already have completed/)).toBeVisible();
  expect(screen.getByRole("button", { name: "Save console cleanup" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Refresh console cleanup" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "Save console cleanup" })).toBeEnabled());
  expect(apiPut).toHaveBeenCalledTimes(1);
});

it("blocks duplicate saves and ignores replies after unmount", async () => {
  vi.mocked(apiGet).mockResolvedValue(policy);
  let resolve!: (_value: unknown) => void;
  vi.mocked(apiPut).mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  const view = render(<ConsoleCleanupPanel />);
  const save = screen.getByRole("button", { name: "Save console cleanup" });
  await waitFor(() => expect(save).toBeEnabled());
  fireEvent.click(save);
  fireEvent.click(save);
  expect(apiPut).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("checkbox")).toBeDisabled();
  const signal = vi.mocked(apiPut).mock.calls[0][2]?.signal;
  view.unmount();
  expect(signal?.aborted).toBe(true);
  resolve(policy);
});

it("ignores a superseded read and aborts work on unmount", async () => {
  let resolve!: (_value: unknown) => void;
  vi.mocked(apiGet)
    .mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    )
    .mockResolvedValue(policy);
  const user = userEvent.setup();
  const view = render(<ConsoleCleanupPanel />);
  await user.click(screen.getByRole("button", { name: "Refresh console cleanup" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "Save console cleanup" })).toBeEnabled());
  resolve({ enabled: true, idle_minutes: 10 });
  await waitFor(() => expect(screen.getByRole("checkbox")).not.toBeChecked());
  view.unmount();
});

it("does not save an old database policy under a changed workspace", async () => {
  vi.mocked(apiGet).mockResolvedValue(policy);
  render(<ConsoleCleanupPanel />);
  await waitFor(() => expect(screen.getByRole("button", { name: "Save console cleanup" })).toBeEnabled());
  vi.mocked(currentWorkspaceBinding).mockReturnValue("other-workspace");
  fireEvent.click(screen.getByRole("button", { name: "Save console cleanup" }));
  expect(await screen.findByText("The database session changed. Refresh cleanup settings.")).toBeVisible();
  expect(apiPut).not.toHaveBeenCalled();
});

it("revokes displayed policy and cancels its save on session invalidation", async () => {
  vi.mocked(apiGet).mockResolvedValue(policy);
  vi.mocked(apiPut).mockImplementation(() => new Promise(() => {}));
  render(<ConsoleCleanupPanel />);
  await waitFor(() => expect(screen.getByRole("button", { name: "Save console cleanup" })).toBeEnabled());
  fireEvent.click(screen.getByRole("button", { name: "Save console cleanup" }));
  invalidateUISession();
  expect(await screen.findByText(/Unlock or reload before changing cleanup settings/)).toBeVisible();
  expect(vi.mocked(apiPut).mock.calls[0][2]?.signal?.aborted).toBe(true);
  expect(screen.getByRole("button", { name: "Save console cleanup" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Refresh console cleanup" }));
  expect(apiGet).toHaveBeenCalledTimes(1);
});
