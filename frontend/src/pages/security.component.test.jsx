import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPost, apiPut } from "../lib/api";
import { SecurityPage } from "./security";

vi.mock("../lib/api", () => ({
  apiDelete: vi.fn(),
  apiGet: vi.fn(),
  apiPost: vi.fn(),
  apiPut: vi.fn(),
}));

const securitySettings = {
  reusable_tokens: false,
  expose_mcp_server_metadata: true,
  mcp_start_enabled: false,
  redaction_mode: "basic",
};

describe("SecurityPage", () => {
  beforeEach(() => {
    apiGet.mockReset();
    apiPost.mockReset();
    apiPut.mockReset();
    apiGet.mockImplementation(async (path) => {
      if (path === "/api/settings/security") return securitySettings;
      if (path === "/api/settings/redaction-rules") return [];
      throw new Error(`unexpected GET ${path}`);
    });
  });

  it("preserves the complete security contract when one toggle changes", async () => {
    const user = userEvent.setup();
    apiPut.mockResolvedValue({ ...securitySettings, reusable_tokens: true });
    render(<SecurityPage />);

    const toggle = await screen.findByRole("checkbox", { name: /Allow reusable token copy/ });
    await user.click(toggle);

    await waitFor(() =>
      expect(apiPut).toHaveBeenCalledWith("/api/settings/security", {
        ...securitySettings,
        reusable_tokens: true,
      }),
    );
    expect(await screen.findByText("Reusable token copy is enabled for newly created tokens.")).toBeVisible();
  });

  it("creates a custom redaction rule and refreshes the rule collection", async () => {
    const user = userEvent.setup();
    apiPost.mockResolvedValue({ ok: true });
    render(<SecurityPage />);

    await user.type(await screen.findByLabelText("Rule name"), "Internal token");
    const pattern = screen.getByLabelText("Regex pattern");
    await user.click(pattern);
    await user.paste("internal_[a-z0-9]{24,}");
    await user.click(screen.getByRole("button", { name: "Add rule" }));

    await waitFor(() =>
      expect(apiPost).toHaveBeenCalledWith("/api/settings/redaction-rules", {
        name: "Internal token",
        pattern: "internal_[a-z0-9]{24,}",
        enabled: true,
      }),
    );
    expect(apiGet).toHaveBeenCalledTimes(3);
  });
});
