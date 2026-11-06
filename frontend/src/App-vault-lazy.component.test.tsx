import { render, screen } from "@testing-library/react";
import { Outlet } from "react-router";
import { expect, it, vi } from "vitest";
import App from "./App";

const state = vi.hoisted(() => ({ unlocked: false, loadVault: vi.fn() }));

vi.mock("./lib/use-unlock-status.ts", () => ({
  useUnlockStatus: () => ({
    unlock: { state: "ready", data: { state: state.unlocked ? "unlocked" : "locked" } },
    loadUnlockStatus: vi.fn(),
  }),
}));
vi.mock("./lib/theme", () => ({ useTheme: () => ({ theme: "dark", setTheme: vi.fn() }) }));
vi.mock("./components/app-shell", () => ({ Shell: () => <Outlet /> }));
vi.mock("./pages/unlock", () => ({ UnlockPage: () => <div>Locked database</div>, UnlockShell: () => null }));
vi.mock("./pages/vault", () => {
  state.loadVault();
  return { VaultPage: () => <div>Vault workspace</div> };
});

it("defers Vault module loading until its unlocked route is opened", async () => {
  const previous = window.location.href;
  window.history.replaceState(null, "", "/vault");
  try {
    const view = render(<App />);
    expect(screen.getByText("Locked database")).toBeVisible();
    expect(state.loadVault).not.toHaveBeenCalled();
    state.unlocked = true;
    view.rerender(<App />);
    expect(await screen.findByText("Vault workspace")).toBeVisible();
    expect(state.loadVault).toHaveBeenCalledOnce();
  } finally {
    state.unlocked = false;
    window.history.replaceState(null, "", previous);
  }
});
