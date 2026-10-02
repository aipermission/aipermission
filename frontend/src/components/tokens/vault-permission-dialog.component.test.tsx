import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPut } from "../../lib/api";
import { VaultPermissionDialog } from "./vault-permission-dialog";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPut: vi.fn() }));

describe("VaultPermissionDialog", () => {
  beforeEach(() => {
    vi.mocked(apiGet).mockImplementation(async (path) => {
      if (path === "/api/tokens/7/project-scopes") {
        return { items: [{ project_id: 3, project_name: "My Project", project_slug: "my-project", enabled: true }], revision: "scope-1" };
      }
      if (path === "/api/tokens/7/project-capabilities") {
        return {
          definitions: [
            {
              name: "vault.inject",
              label: "Inject secrets",
              description: "Inject selected Vault values into a connector session.",
              allowed_rules: ["approval_required", "always_run"],
            },
          ],
          items: [],
          revision: "capability-1",
        };
      }
      throw new Error(`Unexpected GET ${path}`);
    });
    vi.mocked(apiPut).mockResolvedValue({
      definitions: [
        {
          name: "vault.inject",
          label: "Inject secrets",
          description: "Inject selected Vault values into a connector session.",
          allowed_rules: ["approval_required", "always_run"],
        },
      ],
      items: [{ project_id: 3, capability_name: "vault.inject", execution_rule: "always_run", expires_at: null }],
      revision: "capability-2",
    });
  });

  it("preserves autonomous Always grants in the submitted capability payload", async () => {
    const user = userEvent.setup();
    const onSaved = vi.fn();
    render(<VaultPermissionDialog token={{ id: 7, name: "agent" }} onClose={vi.fn()} onSaved={onSaved} />);

    await screen.findByText("Inject secrets");
    await user.click(screen.getByRole("button", { name: "Always" }));
    await user.click(screen.getByRole("button", { name: "Save Vault capabilities" }));

    await waitFor(() =>
      expect(apiPut).toHaveBeenCalledWith(
        "/api/tokens/7/project-capabilities",
        {
          capabilities: [
            {
              project_id: 3,
              capability_name: "vault.inject",
              execution_rule: "always_run",
              expires_at: undefined,
            },
          ],
          expected_revision: "capability-1",
        },
        { signal: expect.any(AbortSignal) },
      ),
    );
    expect(onSaved).toHaveBeenCalledOnce();
  });

  it("persists project visibility and a temporary capability lifetime", async () => {
    const user = userEvent.setup();
    const onSaved = vi.fn();
    vi.mocked(apiPut).mockImplementation(async (path) => {
      if (path === "/api/tokens/7/project-scopes") {
        return { items: [{ project_id: 3, project_name: "My Project", project_slug: "my-project", enabled: false }], revision: "scope-2" };
      }
      return {
        definitions: [
          {
            name: "vault.inject",
            label: "Inject secrets",
            description: "Inject selected Vault values into a connector session.",
            allowed_rules: ["approval_required", "always_run"],
          },
        ],
        items: [{ project_id: 3, capability_name: "vault.inject", execution_rule: "approval_required", expires_at: "future" }],
        revision: "capability-2",
      };
    });
    render(<VaultPermissionDialog token={{ id: 7, name: "agent" }} onClose={vi.fn()} onSaved={onSaved} />);

    await screen.findByText("Inject secrets");
    await user.click(screen.getByRole("checkbox", { name: "My Project project visibility" }));
    await waitFor(() =>
      expect(apiPut).toHaveBeenCalledWith(
        "/api/tokens/7/project-scopes",
        { enabled_project_ids: [], expected_revision: "scope-1" },
        { signal: expect.any(AbortSignal) },
      ),
    );

    await user.click(screen.getByRole("button", { name: "Prompt" }));
    await user.click(screen.getByRole("button", { name: "1h" }));
    await user.click(screen.getByRole("button", { name: "Save Vault capabilities" }));
    await waitFor(() => expect(apiPut).toHaveBeenCalledWith("/api/tokens/7/project-capabilities", expect.anything(), expect.anything()));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
  });

  it("does not submit Vault capabilities loaded for a previously open token", async () => {
    const user = userEvent.setup();
    const firstScopes = deferred();
    const firstCapabilities = deferred();
    let firstSignal: AbortSignal | undefined;
    vi.mocked(apiGet).mockImplementation(async (path, options = {}) => {
      if (path === "/api/tokens/1/project-scopes") {
        firstSignal = options.signal;
        return firstScopes.promise;
      }
      if (path === "/api/tokens/1/project-capabilities") return firstCapabilities.promise;
      if (path === "/api/tokens/2/project-scopes") {
        return { items: [{ project_id: 4, project_name: "Second", project_slug: "second", enabled: true }], revision: "scope-2" };
      }
      if (path === "/api/tokens/2/project-capabilities") {
        return {
          definitions: [{ name: "vault.inject", label: "Inject secrets", description: "Inject values.", allowed_rules: ["always_run"] }],
          items: [],
          revision: "capability-2",
        };
      }
      throw new Error(`Unexpected GET ${path}`);
    });
    vi.mocked(apiPut).mockResolvedValue({ definitions: [], items: [] });

    const view = render(<VaultPermissionDialog token={{ id: 1, name: "first" }} onClose={vi.fn()} onSaved={vi.fn()} />);
    await waitFor(() => expect(firstSignal).toBeDefined());
    view.rerender(<VaultPermissionDialog token={{ id: 2, name: "second" }} onClose={vi.fn()} onSaved={vi.fn()} />);

    await screen.findByText("Second");
    expect(firstSignal?.aborted).toBe(true);
    firstScopes.resolve({ items: [{ project_id: 3, project_name: "First", enabled: true }] });
    firstCapabilities.resolve({
      definitions: [{ name: "vault.inject", label: "Inject secrets", description: "Inject values.", allowed_rules: ["always_run"] }],
      items: [{ project_id: 3, capability_name: "vault.inject", execution_rule: "always_run" }],
    });
    await Promise.resolve();

    await user.click(screen.getByRole("button", { name: "Save Vault capabilities" }));
    await waitFor(() =>
      expect(apiPut).toHaveBeenCalledWith(
        "/api/tokens/2/project-capabilities",
        { capabilities: [], expected_revision: "capability-2" },
        { signal: expect.any(AbortSignal) },
      ),
    );
  });

  it("resets its drafts when closed", () => {
    render(<VaultPermissionDialog token={null} onClose={vi.fn()} onSaved={vi.fn()} />);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("selects a project and removes temporary expiry when Keep is chosen", async () => {
    const user = userEvent.setup();
    render(<VaultPermissionDialog token={{ id: 7, name: "agent" }} onClose={vi.fn()} />);
    await screen.findByText("Inject secrets");
    await user.click(screen.getByRole("button", { name: /My Project.*Vault capabilities/ }));
    await user.click(screen.getByRole("button", { name: "Always" }));
    await user.click(screen.getByRole("button", { name: "1h" }));
    await user.click(screen.getByRole("button", { name: "Keep" }));
    await user.click(screen.getByRole("button", { name: "Save Vault capabilities" }));
    await waitFor(() =>
      expect(apiPut).toHaveBeenCalledWith(
        "/api/tokens/7/project-capabilities",
        {
          capabilities: [{ project_id: 3, capability_name: "vault.inject", execution_rule: "always_run", expires_at: undefined }],
          expected_revision: "capability-1",
        },
        { signal: expect.any(AbortSignal) },
      ),
    );
  });

  it("renders a disabled-only capability when its catalog has no execution rules", async () => {
    const get = vi.mocked(apiGet).getMockImplementation()!;
    vi.mocked(apiGet).mockImplementation(async (path, options) => {
      if (path === "/api/tokens/7/project-capabilities")
        return {
          definitions: [{ name: "vault.inject", label: "Inject secrets", description: "Unavailable for execution.", allowed_rules: [] }],
          items: [],
          revision: "capability-1",
        };
      return get(path, options);
    });
    render(<VaultPermissionDialog token={{ id: 7, name: "agent" }} onClose={vi.fn()} />);
    await screen.findByText("Inject secrets");
    expect(screen.getByRole("button", { name: "Disabled" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Prompt" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Always" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Keep" })).toBeDisabled();
  });

  it("explains when no projects are available for Vault permissions", async () => {
    vi.mocked(apiGet).mockImplementation(async (path) => {
      if (path === "/api/tokens/7/project-scopes") return { items: [], revision: "scope-empty" };
      if (path === "/api/tokens/7/project-capabilities") {
        return { definitions: [], items: [], revision: "capability-empty" };
      }
      throw new Error(`Unexpected GET ${path}`);
    });

    render(<VaultPermissionDialog token={{ id: 7, name: "agent" }} onClose={vi.fn()} onSaved={vi.fn()} />);

    expect(await screen.findByText("Create a project before granting Vault capabilities.")).toBeInTheDocument();
    expect(screen.getByText("0 Vault capability grants selected.")).toBeInTheDocument();
  });

  it("shows a load failure without enabling capability saves", async () => {
    vi.mocked(apiGet).mockRejectedValue(new Error("Vault permissions unavailable"));

    render(<VaultPermissionDialog token={{ id: 7, name: "agent" }} onClose={vi.fn()} onSaved={vi.fn()} />);

    expect(await screen.findByText("Vault permissions unavailable")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save Vault capabilities" })).toBeDisabled();
  });

  it("does not enable saves for a malformed project scope response", async () => {
    vi.mocked(apiGet).mockImplementation(async (path) => {
      if (path === "/api/tokens/7/project-scopes") return { items: [{ project_id: 3, enabled: true }], revision: "scope-1" };
      if (path === "/api/tokens/7/project-capabilities") return { definitions: [], items: [], revision: "capability-1" };
      throw new Error(`Unexpected GET ${path}`);
    });

    render(<VaultPermissionDialog token={{ id: 7, name: "agent" }} onClose={vi.fn()} onSaved={vi.fn()} />);

    expect(await screen.findByText("Invalid project scope response from gateway.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save Vault capabilities" })).toBeDisabled();
  });

  it("does not enable saves for a malformed Vault capability response", async () => {
    vi.mocked(apiGet).mockImplementation(async (path) => {
      if (path === "/api/tokens/7/project-scopes") return { items: [], revision: "scope-1" };
      return { definitions: [{ name: "vault.inject", allowed_rules: ["always_run"] }], items: [], revision: "capability-1" };
    });

    render(<VaultPermissionDialog token={{ id: 7, name: "agent" }} onClose={vi.fn()} onSaved={vi.fn()} />);

    expect(await screen.findByText("Invalid Vault capability response from gateway.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save Vault capabilities" })).toBeDisabled();
  });

  it("does not report a malformed save response as a successful grant", async () => {
    const user = userEvent.setup();
    const onSaved = vi.fn();
    vi.mocked(apiPut).mockResolvedValue({ definitions: [], items: [], revision: "" });
    render(<VaultPermissionDialog token={{ id: 7, name: "agent" }} onClose={vi.fn()} onSaved={onSaved} />);

    await screen.findByText("Inject secrets");
    await user.click(screen.getByRole("button", { name: "Always" }));
    await user.click(screen.getByRole("button", { name: "Save Vault capabilities" }));

    expect(await screen.findByText("Invalid Vault capability response from gateway.")).toBeInTheDocument();
    expect(onSaved).not.toHaveBeenCalled();
  });
});

function deferred() {
  let resolve!: (_value: unknown) => void;
  const promise = new Promise<unknown>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}
