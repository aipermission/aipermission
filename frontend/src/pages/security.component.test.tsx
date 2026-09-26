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

beforeEach(() => {
  vi.mocked(apiGet).mockReset();
  vi.mocked(apiDelete).mockReset();
  vi.mocked(apiPost).mockReset();
  vi.mocked(apiPut).mockReset();
  vi.mocked(apiGet).mockImplementation(async (path) => {
    if (path === "/api/settings/security") return securitySettings;
    if (path === "/api/settings/redaction-rules") return [];
    throw new Error(`unexpected GET ${path}`);
  });
});

describe("SecurityPage", () => {
  it("preserves the complete security contract when one toggle changes", async () => {
    const user = userEvent.setup();
    vi.mocked(apiPut).mockResolvedValue({ ...securitySettings, reusable_tokens: true, revision: "security-2" });
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
    let resolveUpdate: (_value: unknown) => void = () => {
      throw new Error("Uninitialized update request");
    };
    vi.mocked(apiPut).mockImplementation(
      () =>
        new Promise<unknown>((resolve) => {
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
    vi.mocked(apiGet).mockImplementation(async (path) => {
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
    vi.mocked(apiGet).mockImplementation(async (path) => {
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
    vi.mocked(apiGet).mockImplementation(async (path) => {
      if (path === "/api/settings/security") {
        settingsReads += 1;
        return settingsReads === 1 ? securitySettings : { ...securitySettings, redaction_mode: "off", revision: "security-2" };
      }
      if (path === "/api/settings/redaction-rules") return [];
      throw new Error(`unexpected GET ${path}`);
    });
    vi.mocked(apiPut).mockRejectedValue(Object.assign(new Error("settings changed in another client"), { status: 409 }));
    render(<SecurityPage />);

    await user.click(await screen.findByRole("checkbox", { name: /Allow reusable token copy/ }));

    expect(await screen.findByText("settings changed in another client")).toBeVisible();
    await waitFor(() => expect(screen.getByRole("combobox", { name: "Redaction mode" })).toHaveValue("off"));
    expect(settingsReads).toBe(2);
  });

  it("creates a custom redaction rule and refreshes the rule collection", async () => {
    const user = userEvent.setup();
    vi.mocked(apiPost).mockResolvedValue({ ok: true });
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
    vi.mocked(apiGet).mockImplementation(async (path) => {
      if (path === "/api/settings/security") return securitySettings;
      if (path === "/api/settings/redaction-rules") return [rule];
      throw new Error(`unexpected GET ${path}`);
    });
    vi.mocked(apiPut).mockResolvedValue({ ...rule, enabled: false });
    vi.mocked(apiDelete).mockResolvedValue({ ok: true });
    render(<SecurityPage />);

    await screen.findByText("Internal token");
    const enabled = screen.getAllByRole("checkbox", { name: "Enabled" }).at(-1);
    if (!enabled) throw new Error("Missing rule toggle");
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
    vi.mocked(apiGet).mockImplementation(async (path) => {
      if (path === "/api/settings/security") return { ...securitySettings, redaction_mode: "off" };
      if (path === "/api/settings/redaction-rules") return [];
      throw new Error(`unexpected GET ${path}`);
    });
    render(<SecurityPage />);

    expect(await screen.findByText("Custom redaction rules are available when redaction mode is Basic.")).toBeVisible();
    expect(screen.queryByRole("button", { name: "Add rule" })).not.toBeInTheDocument();
  });
});

describe("SecurityPage setting and rule transitions", () => {
  it.each([
    { label: "Allow reusable token copy", setting: "reusable_tokens" },
    { label: "Expose endpoint metadata to MCP", setting: "expose_mcp_server_metadata" },
    { label: "Start MCP execution after unlock", setting: "mcp_start_enabled" },
  ])("persists both states of $label using authoritative revisions", async ({ label, setting }) => {
    const user = userEvent.setup();
    const current = { ...securitySettings, [setting]: false };
    vi.mocked(apiGet).mockImplementation(async (path) => (path === "/api/settings/security" ? current : []));
    vi.mocked(apiPut)
      .mockResolvedValueOnce({ ...current, [setting]: true, revision: "security-2" })
      .mockResolvedValueOnce({ ...current, [setting]: false, revision: "security-3" });
    render(<SecurityPage />);
    const toggle = await screen.findByRole("checkbox", { name: label });
    await waitFor(() => expect(toggle).toBeEnabled());
    await user.click(toggle);
    await waitFor(() => expect(toggle).toBeChecked());
    expect(apiPut).toHaveBeenLastCalledWith(
      "/api/settings/security",
      expect.objectContaining({ [setting]: true, expected_revision: "security-1" }),
    );
    await waitFor(() => expect(toggle).toBeEnabled());
    await user.click(toggle);
    await waitFor(() => expect(toggle).not.toBeChecked());
    expect(apiPut).toHaveBeenLastCalledWith(
      "/api/settings/security",
      expect.objectContaining({ [setting]: false, expected_revision: "security-2" }),
    );
  });

  it.each([new Error("Rules unavailable"), null])(
    "reports an unreadable rule collection without enabling unchecked rules",
    async (failure) => {
      vi.mocked(apiGet).mockImplementation(async (path) => {
        if (path === "/api/settings/security") return securitySettings;
        throw failure;
      });
      render(<SecurityPage />);
      expect(await screen.findByText(failure ? "Rules unavailable" : "Could not load redaction rules.")).toBeVisible();
      expect(screen.queryByRole("button", { name: /^Delete / })).not.toBeInTheDocument();
    },
  );

  it("preserves authoritative settings and permits retry after an ordinary update failure", async () => {
    const user = userEvent.setup();
    vi.mocked(apiPut)
      .mockRejectedValueOnce(new Error("Save unavailable"))
      .mockResolvedValueOnce({ ...securitySettings, reusable_tokens: true, revision: "security-2" });
    render(<SecurityPage />);
    const toggle = await screen.findByRole("checkbox", { name: "Allow reusable token copy" });
    await user.click(toggle);
    expect(await screen.findByText("Save unavailable")).toBeVisible();
    expect(toggle).not.toBeChecked();
    expect(toggle).toBeEnabled();
    expect(screen.getByRole("checkbox", { name: "Expose endpoint metadata to MCP" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Start MCP execution after unlock" })).not.toBeChecked();
    expect(screen.getByRole("combobox", { name: "Redaction mode" })).toHaveValue("basic");
    expect(apiGet).toHaveBeenCalledTimes(2);
    await user.click(toggle);
    await waitFor(() => expect(toggle).toBeChecked());
    expect(apiPut).toHaveBeenCalledTimes(2);
    expect(apiPut).toHaveBeenLastCalledWith("/api/settings/security", {
      reusable_tokens: true,
      expose_mcp_server_metadata: true,
      mcp_start_enabled: false,
      redaction_mode: "basic",
      expected_revision: "security-1",
    });
  });

  it("can enable a disabled rule and create a rule with its enabled flag off", async () => {
    const user = userEvent.setup();
    const rule = { id: 9, name: "Existing rule", pattern: "token_.*", enabled: false };
    let rules = [rule];
    vi.mocked(apiGet).mockImplementation(async (path) => (path === "/api/settings/security" ? securitySettings : rules));
    vi.mocked(apiPut).mockImplementation(async () => {
      rules = [{ ...rule, enabled: true }];
      return rules[0];
    });
    vi.mocked(apiPost).mockImplementation(async () => {
      rules = [...rules, { id: 10, name: "New rule", pattern: "secret.*", enabled: false }];
      return { ok: true };
    });
    render(<SecurityPage />);
    const ruleToggle = await screen.findByRole("checkbox", { name: "Disabled" });
    await user.click(ruleToggle);
    expect(apiPut).toHaveBeenCalledWith("/api/settings/redaction-rules/9", { name: rule.name, pattern: rule.pattern, enabled: true });
    await waitFor(() => expect(ruleToggle).toBeChecked());
    await user.type(screen.getByLabelText("Rule name"), "New rule");
    await user.type(screen.getByLabelText("Regex pattern"), "secret.*");
    const formToggle = screen.getAllByRole("checkbox", { name: "Enabled" })[0];
    if (!formToggle) throw new Error("Missing create-rule enabled flag");
    await user.click(formToggle);
    await user.click(screen.getByRole("button", { name: "Add rule" }));
    expect(apiPost).toHaveBeenCalledWith("/api/settings/redaction-rules", { name: "New rule", pattern: "secret.*", enabled: false });
    expect(await screen.findByText("New rule")).toBeVisible();
    expect(screen.getByRole("checkbox", { name: "Disabled" })).not.toBeChecked();
    expect(screen.getByLabelText("Rule name")).toHaveValue("");
    expect(screen.getByLabelText("Regex pattern")).toHaveValue("");
    expect(formToggle).toBeChecked();
  });

  it("persists both redaction modes and restores the rule editor", async () => {
    const user = userEvent.setup();
    vi.mocked(apiPut)
      .mockResolvedValueOnce({ ...securitySettings, redaction_mode: "off", revision: "security-2" })
      .mockResolvedValueOnce({ ...securitySettings, revision: "security-3" });
    render(<SecurityPage />);
    const mode = await screen.findByRole("combobox", { name: "Redaction mode" });
    await waitFor(() => expect(mode).toBeEnabled());
    await user.selectOptions(mode, "off");
    expect(await screen.findByText("Custom redaction rules are available when redaction mode is Basic.")).toBeVisible();
    expect(mode).toHaveValue("off");
    await waitFor(() => expect(mode).toBeEnabled());
    await user.selectOptions(mode, "basic");
    expect(await screen.findByRole("button", { name: "Add rule" })).toBeVisible();
    expect(mode).toHaveValue("basic");
    expect(apiPut).toHaveBeenLastCalledWith(
      "/api/settings/security",
      expect.objectContaining({ redaction_mode: "basic", expected_revision: "security-2" }),
    );
  });
});
