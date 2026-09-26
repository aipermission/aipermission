import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet } from "../../lib/api";
import { VaultSessionDialog } from "./vault-session-dialog";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn() }));

beforeEach(() => {
  vi.mocked(apiGet).mockReset();
});

it("selects an item beyond the first page and preserves it across searches", async () => {
  const user = userEvent.setup();
  const onStart = vi.fn();
  const first = Array.from({ length: 100 }, (_, index) => ({ id: index + 1, name: `KEY_${index + 1}`, owner_project_id: 4 }));
  vi.mocked(apiGet).mockImplementation(async (path) => {
    const params = new URL(path, "http://localhost").searchParams;
    if (params.get("q")) return { items: [first[0]], total: 1 };
    return {
      items: params.get("offset") === "100" ? [{ id: 101, name: "KEY_101", owner_project_id: 4 }] : first,
      total: 101,
    };
  });
  render(
    <VaultSessionDialog
      state={{
        open: true,
        status: "idle",
        runtime: { id: 2, name: "Test runtime" },
        options: { supported: true, target_project_id: 4, projects: [{ id: 4, name: "My Project" }], defaults: [], items: first },
        sessionOptions: null,
        error: null,
      }}
      onClose={vi.fn()}
      onStart={onStart}
    />,
  );

  await waitFor(() => expect(screen.getByRole("button", { name: "Load more" })).toBeVisible());
  await user.click(screen.getByRole("button", { name: "Load more" }));
  await user.click(await screen.findByRole("checkbox", { name: "Select KEY_101" }));
  await user.type(screen.getByPlaceholderText("Search this project"), "KEY_1");
  await waitFor(() => expect(vi.mocked(apiGet).mock.calls.some(([path]) => String(path).includes("q=KEY_1"))).toBe(true));
  expect(screen.getByText("KEY_101")).toBeVisible();

  await user.click(screen.getByRole("button", { name: "Start session" }));
  expect(onStart).toHaveBeenCalledWith([{ item_id: 101, source_project_id: 4, replace_existing: false }]);
});

it("preserves reviewed default identity and removes its binding revision after an explicit overwrite change", async () => {
  const user = userEvent.setup();
  const onStart = vi.fn();
  vi.mocked(apiGet).mockResolvedValue({ items: [{ id: 7, name: "PROJECT_API_KEY", owner_project_id: 4 }], total: 1 });
  render(
    <VaultSessionDialog
      state={{
        open: true,
        status: "idle",
        runtime: { id: 2, name: "Test runtime" },
        sessionOptions: null,
        error: null,
        options: {
          supported: true,
          target_project_id: 4,
          items: [],
          projects: [{ id: 4, name: "My Project" }],
          defaults: [
            {
              id: 3,
              vault_item_id: 7,
              vault_item_name: "PROJECT_API_KEY",
              source_project_id: 4,
              source_project_name: "My Project",
              replace_existing: true,
              binding_revision: 2,
            },
          ],
        },
      }}
      onClose={vi.fn()}
      onStart={onStart}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Start session" }));
  expect(onStart).toHaveBeenLastCalledWith([
    { item_id: 7, source_project_id: 4, replace_existing: true, binding_id: 3, binding_revision: 2 },
  ]);
  await user.click(screen.getByRole("checkbox", { name: "Overwrite existing shell value" }));
  await user.click(screen.getByRole("button", { name: "Start session" }));
  expect(onStart).toHaveBeenLastCalledWith([
    { item_id: 7, source_project_id: 4, replace_existing: false, binding_id: undefined, binding_revision: undefined },
  ]);
  await user.click(screen.getByRole("button", { name: "Remove PROJECT_API_KEY" }));
  await user.click(screen.getByRole("button", { name: "Start session" }));
  expect(onStart).toHaveBeenLastCalledWith([]);
});

it("binds an explicitly selected shared item to the selected source project rather than an unrelated default binding", async () => {
  const user = userEvent.setup();
  const onStart = vi.fn();
  const item = { id: 7, name: "PROJECT_API_KEY", owner_project_id: 4, project_ids: [5] };
  vi.mocked(apiGet).mockResolvedValue({ items: [item], total: 1 });
  render(
    <VaultSessionDialog
      state={{
        open: true,
        status: "idle",
        runtime: { id: 2, name: "Test runtime" },
        sessionOptions: null,
        error: null,
        options: {
          supported: true,
          target_project_id: 4,
          items: [item],
          projects: [
            { id: 4, name: "My Project" },
            { id: 5, name: "Shared Project" },
          ],
          defaults: [
            {
              id: 3,
              vault_item_id: 7,
              vault_item_name: item.name,
              source_project_id: 4,
              source_project_name: "My Project",
              replace_existing: false,
              binding_revision: 2,
            },
          ],
        },
      }}
      onClose={vi.fn()}
      onStart={onStart}
    />,
  );
  await user.selectOptions(screen.getByRole("combobox", { name: "Project" }), "5");
  const checkbox = await screen.findByRole("checkbox", { name: "Select PROJECT_API_KEY" });
  await user.click(checkbox);
  await user.click(checkbox);
  await user.click(screen.getByRole("button", { name: "Start session" }));
  expect(onStart).toHaveBeenCalledWith([{ item_id: 7, source_project_id: 5, replace_existing: false }]);
});
