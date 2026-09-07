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
      expect(apiPut).toHaveBeenCalledWith("/api/tokens/7/project-capabilities", {
        capabilities: [
          {
            project_id: 3,
            capability_name: "vault.inject",
            execution_rule: "always_run",
            expires_at: undefined,
          },
        ],
      }),
    );
    expect(onSaved).toHaveBeenCalledOnce();
  });
});
