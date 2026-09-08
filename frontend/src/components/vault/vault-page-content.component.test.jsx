import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { VaultFilters, VaultItemsTable, VaultPageHeader } from "./vault-page-content";

vi.mock("./vault-row", () => ({
  VaultRow: ({ item, onEdit, onReveal, onReplace, onBindings, onDelete }) => (
    <tr>
      <td>{item.name}</td>
      <td>
        {[onEdit, onReveal, onReplace, onBindings, onDelete].map((callback, index) => (
          <button key={index} type="button" onClick={callback}>
            action-{index}
          </button>
        ))}
      </td>
    </tr>
  ),
}));

it("wires Vault header and filter controls", async () => {
  const user = userEvent.setup();
  const onRefresh = vi.fn();
  const onCreate = vi.fn();
  const onChange = vi.fn();
  render(
    <>
      <VaultPageHeader loading={false} canCreate onRefresh={onRefresh} onCreate={onCreate} />
      <VaultFilters filters={{ project_id: "", query: "", expiry: "all" }} projects={[{ id: 3, name: "My Project" }]} onChange={onChange} />
    </>,
  );

  await user.click(screen.getByRole("button", { name: "Refresh" }));
  await user.click(screen.getByRole("button", { name: "Add vault item" }));
  await user.selectOptions(screen.getByDisplayValue("All projects"), "3");
  await user.type(screen.getByPlaceholderText(/Search name/), "token");
  await user.selectOptions(screen.getByDisplayValue("All expiry states"), "warning");

  expect(onRefresh).toHaveBeenCalledOnce();
  expect(onCreate).toHaveBeenCalledOnce();
  expect(onChange).toHaveBeenCalledWith({ project_id: "3" });
  expect(onChange).toHaveBeenCalledWith({ query: "t" });
  expect(onChange).toHaveBeenCalledWith({ expiry: "warning" });
});

it("renders Vault rows and forwards every row action", async () => {
  const user = userEvent.setup();
  const actions = Array.from({ length: 5 }, () => vi.fn());
  const item = { id: 7, name: "DEPLOY_TOKEN" };
  const { rerender } = render(
    <VaultItemsTable
      items={{ state: "ready" }}
      visibleItems={[item]}
      projects={[]}
      onEdit={actions[0]}
      onReveal={actions[1]}
      onReplace={actions[2]}
      onBindings={actions[3]}
      onDelete={actions[4]}
    />,
  );

  for (let index = 0; index < actions.length; index += 1) {
    await user.click(screen.getByRole("button", { name: `action-${index}` }));
    expect(actions[index]).toHaveBeenCalledWith(item);
  }

  rerender(
    <VaultItemsTable
      items={{ state: "loading" }}
      visibleItems={[]}
      projects={[]}
      onEdit={vi.fn()}
      onReveal={vi.fn()}
      onReplace={vi.fn()}
      onBindings={vi.fn()}
      onDelete={vi.fn()}
    />,
  );
  expect(screen.getByText("Loading Vault metadata...")).toBeVisible();

  rerender(
    <VaultItemsTable
      items={{ state: "ready" }}
      visibleItems={[]}
      projects={[]}
      onEdit={vi.fn()}
      onReveal={vi.fn()}
      onReplace={vi.fn()}
      onBindings={vi.fn()}
      onDelete={vi.fn()}
    />,
  );
  expect(screen.getByText("No Vault items match this view.")).toBeVisible();
});
