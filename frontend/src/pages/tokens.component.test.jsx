import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
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
});
