import { useState } from "react";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import App from "./App";
import { apiGet } from "./lib/api";

vi.mock("./lib/api", async (importOriginal) => ({ ...(await importOriginal()), apiGet: vi.fn() }));
vi.mock("./lib/theme", () => ({ useTheme: () => ({ theme: "dark", setTheme: vi.fn() }) }));
vi.mock("./pages/settings", () => ({ SettingsPage: () => null }));
vi.mock("./components/app-shell", () => ({ Shell: () => <span>Unlocked workspace</span> }));
vi.mock("./pages/unlock", () => ({
  UnlockShell: ({ title, children }) => (
    <div>
      <span>{title}</span>
      {children}
    </div>
  ),
  UnlockPage: ({ onUnlocked }) => {
    const [error, setError] = useState("");
    return (
      <div>
        <button type="button" onClick={() => onUnlocked(new AbortController().signal).catch((failure) => setError(failure.message))}>
          Refresh unlock status
        </button>
        {error ? <span>{error}</span> : null}
      </div>
    );
  },
}));

beforeEach(() => apiGet.mockReset());

it("loads unlock status and forwards lifecycle cancellation to reconciliation", async () => {
  const user = userEvent.setup();
  apiGet.mockResolvedValue({ state: "session_required", databases: [] });

  render(<App />);

  expect(screen.getByText("Checking encrypted database...")).toBeVisible();
  await user.click(await screen.findByRole("button", { name: "Refresh unlock status" }));

  await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(2));
  expect(apiGet.mock.calls[1]).toEqual(["/api/unlock/status", { signal: expect.any(AbortSignal) }]);
});

it("keeps the unlock workflow mounted when lifecycle status reconciliation fails", async () => {
  const user = userEvent.setup();
  apiGet.mockResolvedValueOnce({ state: "session_required", databases: [] }).mockRejectedValueOnce(new Error("Status unavailable"));

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
  apiGet
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
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}
