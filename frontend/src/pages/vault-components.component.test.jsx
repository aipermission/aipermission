import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { VaultBindingsDialog } from "./vault-components";

const target = { id: 7, name: "Target", connector_kind: "ssh", profiles: [{ id: 8, label: "Primary" }] };
const item = { id: 5, name: "API_KEY", owner_project_id: 2, project_ids: [2] };
const project = { id: 2, name: "My Project" };

function BindingHarness({ existing = false, onSave = vi.fn() }) {
  const [state, setState] = useState({
    open: true,
    item,
    state: "ready",
    data: existing ? [{ id: 11, source_project_id: 2, target_id: 7, profile_id: 8, replace_existing: true, target_name: "Target" }] : [],
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
