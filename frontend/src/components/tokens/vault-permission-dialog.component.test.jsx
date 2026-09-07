import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPut } from "../../lib/api";
import { VaultPermissionDialog } from "./vault-permission-dialog";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPut: vi.fn() }));

describe("VaultPermissionDialog", () => {
  beforeEach(() => {
    apiGet.mockImplementation(async (path) => {
      if (path === "/api/tokens/7/project-scopes") {
        return { items: [{ project_id: 3, project_name: "My Project", enabled: true }] };
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
        };
      }
      throw new Error(`Unexpected GET ${path}`);
    });
    apiPut.mockResolvedValue({
      definitions: [
        {
          name: "vault.inject",
          label: "Inject secrets",
          description: "Inject selected Vault values into a connector session.",
          allowed_rules: ["approval_required", "always_run"],
        },
      ],
      items: [{ project_id: 3, capability: "vault.inject", execution_rule: "always_run", expires_at: null }],
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
        },
        { signal: expect.any(AbortSignal) },
      ),
    );
    expect(onSaved).toHaveBeenCalledOnce();
  });

  it("does not submit Vault capabilities loaded for a previously open token", async () => {
    const user = userEvent.setup();
    const firstScopes = deferred();
    const firstCapabilities = deferred();
    let firstSignal;
    apiGet.mockImplementation(async (path, options = {}) => {
      if (path === "/api/tokens/1/project-scopes") {
        firstSignal = options.signal;
        return firstScopes.promise;
      }
      if (path === "/api/tokens/1/project-capabilities") return firstCapabilities.promise;
      if (path === "/api/tokens/2/project-scopes") return { items: [{ project_id: 4, project_name: "Second", enabled: true }] };
      if (path === "/api/tokens/2/project-capabilities") {
        return {
          definitions: [{ name: "vault.inject", label: "Inject secrets", description: "Inject values.", allowed_rules: ["always_run"] }],
          items: [],
        };
      }
      throw new Error(`Unexpected GET ${path}`);
    });
    apiPut.mockResolvedValue({ definitions: [], items: [] });

    const view = render(<VaultPermissionDialog token={{ id: 1, name: "first" }} onClose={vi.fn()} onSaved={vi.fn()} />);
    await waitFor(() => expect(firstSignal).toBeDefined());
    view.rerender(<VaultPermissionDialog token={{ id: 2, name: "second" }} onClose={vi.fn()} onSaved={vi.fn()} />);

    await screen.findByText("Second");
    expect(firstSignal.aborted).toBe(true);
    firstScopes.resolve({ items: [{ project_id: 3, project_name: "First", enabled: true }] });
    firstCapabilities.resolve({
      definitions: [{ name: "vault.inject", label: "Inject secrets", description: "Inject values.", allowed_rules: ["always_run"] }],
      items: [{ project_id: 3, capability_name: "vault.inject", execution_rule: "always_run" }],
    });
    await Promise.resolve();

    await user.click(screen.getByRole("button", { name: "Save Vault capabilities" }));
    await waitFor(() =>
      expect(apiPut).toHaveBeenCalledWith("/api/tokens/2/project-capabilities", { capabilities: [] }, { signal: expect.any(AbortSignal) }),
    );
  });
});

function deferred() {
  let resolve;
  const promise = new Promise((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}
