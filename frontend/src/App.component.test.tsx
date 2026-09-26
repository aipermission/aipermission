import { useState } from "react";
import type { ComponentProps } from "react";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import App from "./App";
import { apiGet } from "./lib/api";
import { errorMessage } from "./lib/errors.ts";
import type { UnlockPage, UnlockShell } from "./pages/unlock.tsx";

vi.mock("./lib/api", async (importOriginal) => ({ ...(await importOriginal<typeof import("./lib/api")>()), apiGet: vi.fn() }));
vi.mock("./lib/theme", () => ({ useTheme: () => ({ theme: "dark", setTheme: vi.fn() }) }));
vi.mock("./pages/settings", () => ({ SettingsPage: () => null }));
vi.mock("./components/app-shell", () => ({ Shell: () => <span>Unlocked workspace</span> }));
vi.mock("./pages/unlock", () => ({
  UnlockShell: ({ title, children }: ComponentProps<typeof UnlockShell>) => (
    <div>
      <span>{title}</span>
      {children}
    </div>
  ),
  UnlockPage: ({ onUnlocked }: ComponentProps<typeof UnlockPage>) => {
    const [error, setError] = useState("");
    return (
      <div>
        <button
          type="button"
          onClick={() =>
            Promise.resolve(onUnlocked(new AbortController().signal)).catch((failure: unknown) => setError(errorMessage(failure)))
          }
        >
          Refresh unlock status
        </button>
        {error ? <span>{error}</span> : null}
      </div>
    );
  },
}));

const get = vi.mocked(apiGet);
beforeEach(() => get.mockReset());

it("loads unlock status and forwards lifecycle cancellation to reconciliation", async () => {
  const user = userEvent.setup();
  get.mockResolvedValue({ state: "session_required", databases: [] });

  render(<App />);

  expect(screen.getByText("Checking encrypted database...")).toBeVisible();
  await user.click(await screen.findByRole("button", { name: "Refresh unlock status" }));

  await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(2));
  expect(get.mock.calls[1]).toEqual(["/api/unlock/status", { signal: expect.any(AbortSignal) }]);
});

it("keeps the unlock workflow mounted when lifecycle status reconciliation fails", async () => {
  const user = userEvent.setup();
  get.mockResolvedValueOnce({ state: "session_required", databases: [] }).mockRejectedValueOnce(new Error("Status unavailable"));

  render(<App />);
  await user.click(await screen.findByRole("button", { name: "Refresh unlock status" }));

  expect(await screen.findByText("Status unavailable")).toBeVisible();
  expect(screen.getByRole("button", { name: "Refresh unlock status" })).toBeVisible();
  expect(screen.queryByText("Gateway unavailable")).not.toBeInTheDocument();
});

it("ignores an older background status response after lifecycle reconciliation", async () => {
  const user = userEvent.setup();
  const background = deferred();
  const lifecycle = deferred();
  get
    .mockResolvedValueOnce({ state: "session_required", databases: [] })
    .mockReturnValueOnce(background.promise)
    .mockReturnValueOnce(lifecycle.promise);

  render(<App />);
  const refresh = await screen.findByRole("button", { name: "Refresh unlock status" });
  act(() => window.dispatchEvent(new Event("aipermission:ui-session-required")));
  await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(2));
  await user.click(refresh);
  await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(3));

  await act(async () => lifecycle.resolve({ state: "unlocked", databases: [] }));
  expect(await screen.findByText("Unlocked workspace")).toBeVisible();
  await act(async () => background.resolve({ state: "session_required", databases: [] }));
  expect(screen.getByText("Unlocked workspace")).toBeVisible();
  expect(screen.queryByRole("button", { name: "Refresh unlock status" })).not.toBeInTheDocument();
});

function deferred() {
  let resolve!: (_value: unknown) => void;
  let reject!: (_error: unknown) => void;
  const promise = new Promise<unknown>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

it.each([
  { databases: [] },
  { state: "", databases: [] },
  { state: "unlocked", databases: [{ id: "db", name: "Default", unlocked: "yes" }] },
])("rejects malformed unlock status without exposing the workspace: %j", async (response) => {
  get.mockResolvedValueOnce(response);
  render(<App />);
  expect(await screen.findByText("Gateway unavailable")).toBeVisible();
  expect(screen.queryByText("Unlocked workspace")).not.toBeInTheDocument();
});

it("keeps the unlock form available after malformed lifecycle reconciliation", async () => {
  const user = userEvent.setup();
  get.mockResolvedValueOnce({ state: "session_required", databases: [] }).mockResolvedValueOnce({ state: true });
  render(<App />);
  await user.click(await screen.findByRole("button", { name: "Refresh unlock status" }));
  expect(await screen.findByText("Invalid database status response.")).toBeVisible();
  expect(screen.getByRole("button", { name: "Refresh unlock status" })).toBeVisible();
});
