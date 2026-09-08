import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import App from "./App";
import { apiGet } from "./lib/api";

vi.mock("./lib/api", async (importOriginal) => ({ ...(await importOriginal()), apiGet: vi.fn() }));
vi.mock("./lib/theme", () => ({ useTheme: () => ({ theme: "dark", setTheme: vi.fn() }) }));
vi.mock("./pages/settings", () => ({ SettingsPage: () => null }));
vi.mock("./pages/unlock", () => ({
  UnlockShell: ({ title, children }) => (
    <div>
      <span>{title}</span>
      {children}
    </div>
  ),
  UnlockPage: ({ onUnlocked }) => (
    <button type="button" onClick={() => onUnlocked(new AbortController().signal)}>
      Refresh unlock status
    </button>
  ),
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
