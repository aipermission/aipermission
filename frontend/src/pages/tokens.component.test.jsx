import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { apiPost } from "../lib/api";
import { TokensPage } from "./tokens";

const gateway = {
  tokens: {
    state: "ready",
    data: [{ id: 7, name: "maintenance", token: "aip_masked", created_at: "2026-08-31T00:00:00Z" }],
    error: null,
  },
  loadTokens: vi.fn(async () => gateway.tokens.data),
  loadTargets: vi.fn(async () => []),
};

vi.mock("../lib/api", async () => ({ ...(await vi.importActual("../lib/api")), apiPost: vi.fn() }));
vi.mock("../lib/gateway-context", () => ({ useGateway: () => gateway }));
vi.mock("../lib/use-connector-permissions", () => ({
  useConnectorPermissions: () => ({
    connectorPermissionState: { state: "ready", data: {}, error: null },
    loadAllConnectorPermissions: vi.fn(async () => ({})),
  }),
}));

describe("TokensPage", () => {
  beforeEach(() => {
    apiPost.mockReset();
    gateway.loadTokens.mockClear();
    gateway.loadTargets.mockClear();
    gateway.tokens.data = [{ id: 7, name: "maintenance", token: "aip_masked", created_at: "2026-08-31T00:00:00Z" }];
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("keeps successful revoke feedback visible after closing the dialog", async () => {
    const user = userEvent.setup();
    apiPost.mockResolvedValue({ ok: true });
    render(<TokensPage />);

    await user.click(screen.getByRole("button", { name: "Revoke" }));
    await user.click(screen.getByRole("button", { name: "Revoke token" }));

    expect(await screen.findByText("maintenance revoked.")).toBeVisible();
    expect(apiPost).toHaveBeenCalledWith("/api/tokens/7/revoke", {});
  });

  it("creates a short-lived token from the drawer", async () => {
    const user = userEvent.setup();
    apiPost.mockResolvedValue({ id: 8, name: "review-agent", token: "aip_new" });
    render(<TokensPage />);

    await user.click(screen.getByRole("button", { name: "Add token" }));
    const name = screen.getByLabelText("Name");
    await user.clear(name);
    await user.type(name, "review-agent");
    await user.selectOptions(screen.getByLabelText("Expiration"), "1h");
    await user.click(screen.getByRole("button", { name: "Create token" }));

    expect(apiPost).toHaveBeenCalledWith("/api/tokens", expect.objectContaining({ name: "review-agent", expires_at: expect.any(String) }));
    expect(await screen.findByText("Token created.")).toBeVisible();
    expect(gateway.loadTokens).toHaveBeenCalled();
  });

  it("moves a token to expired state at its expiry boundary", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-09-21T12:00:00.000Z"));
    gateway.tokens.data = [
      {
        id: 8,
        name: "short-lived",
        token: "aip_short",
        created_at: "2026-09-21T11:00:00.000Z",
        expires_at: "2026-09-21T12:00:01.000Z",
      },
    ];
    render(<TokensPage />);

    expect(screen.getByText("short-lived")).toBeVisible();
    expect(screen.getByRole("button", { name: /Active 1/ })).toBeVisible();
    await act(async () => vi.advanceTimersByTimeAsync(1001));

    expect(screen.queryByText("short-lived")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Expired 1/ })).toBeVisible();
  });
  it("filters expired and revoked tokens without enabling their actions", async () => {
    const user = userEvent.setup();
    gateway.tokens.data = [
      { id: 8, name: "expired-agent", token: "aip_expired", expires_at: "2020-01-01T00:00:00Z" },
      { id: 9, name: "revoked-agent", token: "aip_revoked", revoked_at: "2026-09-01T00:00:00Z" },
    ];
    render(<TokensPage />);

    await user.click(screen.getByRole("button", { name: /Expired 1/ }));
    expect(screen.getByText("expired-agent")).toBeVisible();
    expect(screen.getByRole("button", { name: "Connectors" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: /Revoked 1/ }));
    expect(screen.getByText("revoked-agent")).toBeVisible();
    expect(screen.getByRole("button", { name: "Revoke" })).toBeDisabled();
  });
});
