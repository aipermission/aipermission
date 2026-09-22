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
const connectorPermissions = {
  state: { state: "ready", data: {}, error: null },
  load: vi.fn(async () => ({})),
};

vi.mock("../lib/api", async () => ({ ...(await vi.importActual("../lib/api")), apiPost: vi.fn() }));
vi.mock("../lib/gateway-context", () => ({ useGateway: () => gateway }));
vi.mock("../lib/use-connector-permissions", () => ({
  useConnectorPermissions: () => ({
    connectorPermissionState: connectorPermissions.state,
    loadAllConnectorPermissions: connectorPermissions.load,
  }),
}));

function deferred() {
  let resolve;
  const promise = new Promise((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

describe("TokensPage", () => {
  beforeEach(() => {
    apiPost.mockReset();
    gateway.loadTokens.mockClear();
    gateway.loadTargets.mockClear();
    connectorPermissions.load.mockClear();
    connectorPermissions.state = { state: "ready", data: {}, error: null };
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

  it("keeps an in-flight token drawer open and surfaces the show-once token", async () => {
    const user = userEvent.setup();
    const creation = deferred();
    apiPost.mockReturnValueOnce(creation.promise);
    render(<TokensPage />);

    await user.click(screen.getByRole("button", { name: "Add token" }));
    const oldName = screen.getByLabelText("Name");
    await user.clear(oldName);
    await user.type(oldName, "old-agent");
    await user.click(screen.getByRole("button", { name: "Create token" }));
    expect(screen.getByRole("button", { name: "Close drawer" })).toBeDisabled();
    await user.keyboard("{Escape}");
    expect(screen.getByRole("heading", { name: "Add API token" })).toBeVisible();
    await act(async () => creation.resolve({ id: 8, name: "old-agent", token: "aip_old_secret" }));

    expect(screen.queryByRole("heading", { name: "Add API token" })).not.toBeInTheDocument();
    expect(screen.getByText("old-agent token created.")).toBeVisible();
    expect(screen.getByDisplayValue("aip_old_secret")).toBeVisible();
    expect(screen.getByText("Token created.")).toBeVisible();
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

  it("refreshes tokens, targets, and connector permissions together", async () => {
    const user = userEvent.setup();
    const refreshed = [{ id: 11, name: "refreshed", token: "aip_refreshed" }];
    gateway.loadTokens.mockResolvedValueOnce(refreshed);
    render(<TokensPage />);

    await user.click(screen.getByRole("button", { name: "Refresh" }));

    expect(gateway.loadTargets).toHaveBeenCalledOnce();
    expect(connectorPermissions.load).toHaveBeenCalledWith(refreshed);
  });

  it("filters active tokens and restores the complete token list", async () => {
    const user = userEvent.setup();
    gateway.tokens.data = [
      { id: 7, name: "active-agent", token: "aip_active" },
      { id: 8, name: "expired-agent", token: "aip_expired", expires_at: "2020-01-01T00:00:00Z" },
    ];
    render(<TokensPage />);

    expect(screen.getByText("active-agent")).toBeVisible();
    expect(screen.queryByText("expired-agent")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /Total tokens 2/ }));
    expect(screen.getByText("expired-agent")).toBeVisible();
    await user.click(screen.getByRole("button", { name: /Active 1/ }));
    expect(screen.queryByText("expired-agent")).not.toBeInTheDocument();
  });

  it("summarizes active connector grants by kind and target profile", () => {
    connectorPermissions.state = {
      state: "ready",
      error: null,
      data: {
        7: [
          { connector_kind: "postgres", target_id: 2, profile_id: 3, execution_rule: "always_run" },
          { connector_kind: "postgres", target_id: 2, profile_id: 3, execution_rule: "approval_required" },
          { connector_kind: "ssh", target_id: 4, profile_id: 5, execution_rule: "blocked" },
        ],
      },
    };

    render(<TokensPage />);

    expect(screen.getByText("postgres")).toBeVisible();
    expect(screen.getByText("ssh")).toBeVisible();
    expect(screen.getByText("3 action grants / 2 target profiles")).toBeVisible();
  });
});
