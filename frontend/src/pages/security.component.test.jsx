import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiDelete, apiGet, apiPost, apiPut } from "../lib/api";
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
  revision: "security-1",
};

describe("SecurityPage", () => {
  beforeEach(() => {
    apiGet.mockReset();
    apiDelete.mockReset();
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
    apiPut.mockResolvedValue({ ...securitySettings, reusable_tokens: true, revision: "security-2" });
    render(<SecurityPage />);

    const toggle = await screen.findByRole("checkbox", { name: /Allow reusable token copy/ });
    await user.click(toggle);

    await waitFor(() =>
      expect(apiPut).toHaveBeenCalledWith("/api/settings/security", {
        reusable_tokens: true,
        expose_mcp_server_metadata: true,
        mcp_start_enabled: false,
        redaction_mode: "basic",
        expected_revision: "security-1",
      }),
    );
    expect(await screen.findByText("Reusable token copy is enabled for newly created tokens.")).toBeVisible();
  });

  it("locks every security setting while a settings update is pending", async () => {
    const user = userEvent.setup();
    let resolveUpdate;
    apiPut.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveUpdate = resolve;
        }),
    );
    render(<SecurityPage />);

    const toggle = await screen.findByRole("checkbox", { name: /Allow reusable token copy/ });
    const mode = screen.getByRole("combobox", { name: "Redaction mode" });
    await user.click(toggle);

    expect(toggle).toBeDisabled();
    expect(mode).toBeDisabled();
    await user.selectOptions(mode, "off");
    expect(apiPut).toHaveBeenCalledTimes(1);

    resolveUpdate({ ...securitySettings, reusable_tokens: true, revision: "security-2" });
    await waitFor(() => expect(toggle).toBeEnabled());
  });

  it("keeps security settings disabled when their initial load fails", async () => {
    apiGet.mockImplementation(async (path) => {
      if (path === "/api/settings/security") throw new Error("settings unavailable");
      if (path === "/api/settings/redaction-rules") return [];
      throw new Error(`unexpected GET ${path}`);
    });
    render(<SecurityPage />);

    expect(await screen.findByText("settings unavailable")).toBeVisible();
    expect(screen.getByRole("checkbox", { name: /Allow reusable token copy/ })).toBeDisabled();
    expect(screen.getByRole("combobox", { name: "Redaction mode" })).toBeDisabled();
  });

  it("rejects a malformed successful settings response", async () => {
    apiGet.mockImplementation(async (path) => {
      if (path === "/api/settings/security") return { ...securitySettings, revision: "" };
      if (path === "/api/settings/redaction-rules") return [];
      throw new Error(`unexpected GET ${path}`);
    });
    render(<SecurityPage />);

    expect(await screen.findByText("Security settings response is invalid.")).toBeVisible();
    expect(screen.getByRole("checkbox", { name: /Allow reusable token copy/ })).toBeDisabled();
    expect(screen.getByRole("combobox", { name: "Redaction mode" })).toBeDisabled();
  });

  it("reloads authoritative settings after a revision conflict", async () => {
    const user = userEvent.setup();
    let settingsReads = 0;
    apiGet.mockImplementation(async (path) => {
      if (path === "/api/settings/security") {
        settingsReads += 1;
        return settingsReads === 1 ? securitySettings : { ...securitySettings, redaction_mode: "off", revision: "security-2" };
      }
      if (path === "/api/settings/redaction-rules") return [];
      throw new Error(`unexpected GET ${path}`);
    });
    apiPut.mockRejectedValue(Object.assign(new Error("settings changed in another client"), { status: 409 }));
    render(<SecurityPage />);

    await user.click(await screen.findByRole("checkbox", { name: /Allow reusable token copy/ }));

    expect(await screen.findByText("settings changed in another client")).toBeVisible();
    await waitFor(() => expect(screen.getByRole("combobox", { name: "Redaction mode" })).toHaveValue("off"));
    expect(settingsReads).toBe(2);
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

  it("updates and deletes an existing custom redaction rule", async () => {
    const user = userEvent.setup();
    const rule = { id: 9, name: "Internal token", pattern: "internal_[a-z0-9]+", enabled: true };
    apiGet.mockImplementation(async (path) => {
      if (path === "/api/settings/security") return securitySettings;
      if (path === "/api/settings/redaction-rules") return [rule];
      throw new Error(`unexpected GET ${path}`);
    });
    apiPut.mockResolvedValue({ ...rule, enabled: false });
    apiDelete.mockResolvedValue({ ok: true });
    render(<SecurityPage />);

    await screen.findByText("Internal token");
    const enabled = screen.getAllByRole("checkbox", { name: "Enabled" }).at(-1);
    await user.click(enabled);
    await waitFor(() =>
      expect(apiPut).toHaveBeenCalledWith("/api/settings/redaction-rules/9", {
        name: rule.name,
        pattern: rule.pattern,
        enabled: false,
      }),
    );

    await user.click(screen.getByRole("button", { name: "Delete Internal token" }));
    await waitFor(() => expect(apiDelete).toHaveBeenCalledWith("/api/settings/redaction-rules/9"));
  });

  it("hides custom rule editing when redaction is off", async () => {
    apiGet.mockImplementation(async (path) => {
      if (path === "/api/settings/security") return { ...securitySettings, redaction_mode: "off" };
      if (path === "/api/settings/redaction-rules") return [];
      throw new Error(`unexpected GET ${path}`);
    });
    render(<SecurityPage />);

    expect(await screen.findByText("Custom redaction rules are available when redaction mode is Basic.")).toBeVisible();
    expect(screen.queryByRole("button", { name: "Add rule" })).not.toBeInTheDocument();
  });
});
