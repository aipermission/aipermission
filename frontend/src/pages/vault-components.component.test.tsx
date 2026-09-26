import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { emptyVaultEditor } from "../components/vault/use-vault-collection";
import { VaultBindingsDialog, VaultEditor } from "./vault-components";
import type { VaultBindingsState } from "../components/vault/use-vault-bindings.ts";

const target = { id: 7, name: "Target", connector_kind: "fixture", profiles: [{ id: 8, label: "Primary", vault_session_supported: true }] };
const item = { id: 5, name: "API_KEY", owner_project_id: 2, project_ids: [2] };
const project = { id: 2, name: "My Project" };

function BindingHarness({ existing = false, saving = false, onSave = vi.fn() }: { existing?: boolean; saving?: boolean; onSave?: (_state: VaultBindingsState) => void }) {
  const [state, setState] = useState<VaultBindingsState>({
    open: true,
    item,
    state: saving ? "saving" : "ready",
    data: existing ? [{ id: 11, vault_item_id: 5, source_project_id: 2, target_id: 7, profile_id: 8,
      binding_revision: 1, replace_existing: true, target_name: "Target", profile_label: "Primary", source_project_name: "My Project", connector_kind: "fixture" }] : [],
    targets: [target],
    source_project_id: "2",
    target_id: "7",
    profile_id: "8",
    replace_existing: false,
    error: null,
  });
  return (
    <VaultBindingsDialog
      state={state}
      projects={[project]}
      onChange={setState}
      onClose={vi.fn()}
      onSave={(event) => {
        event.preventDefault();
        onSave(state);
      }}
      onDelete={vi.fn()}
    />
  );
}

it("preserves an overwrite choice for a new Vault binding", async () => {
  const user = userEvent.setup();
  const onSave = vi.fn();
  render(<BindingHarness onSave={onSave} />);

  const overwrite = screen.getByRole("checkbox", { name: /Overwrite an existing shell value/ });
  await user.click(overwrite);
  expect(overwrite).toBeChecked();
  await user.click(screen.getByRole("button", { name: "Add binding" }));
  expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ replace_existing: true }));
});

it("preserves an overwrite change for an existing Vault binding", async () => {
  const user = userEvent.setup();
  const onSave = vi.fn();
  render(<BindingHarness existing onSave={onSave} />);

  const overwrite = screen.getByRole("checkbox", { name: /Overwrite an existing shell value/ });
  expect(overwrite).toBeChecked();
  await user.click(overwrite);
  expect(overwrite).not.toBeChecked();
  await user.click(screen.getByRole("button", { name: "Update binding" }));
  expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ replace_existing: false }));
});

it("disables binding removals while another binding mutation is pending", () => {
  render(<BindingHarness existing saving />);
  expect(screen.getByRole("button", { name: "Remove binding" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Saving..." })).toBeDisabled();
});

it("keeps the Vault editor fields locked while saving but permits cancellation", () => {
  render(
    <VaultEditor
      editor={{ ...emptyVaultEditor, open: true, name: "API_KEY", value: "secret", owner_project_id: "2" }}
      projects={[project]}
      action={{ state: "saving", error: null }}
      onChange={vi.fn()}
      onClose={vi.fn()}
      onSubmit={vi.fn()}
    />,
  );
  expect(screen.getByRole("textbox", { name: /Environment name/ })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Import value" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Saving..." })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Cancel" })).toBeEnabled();
});
