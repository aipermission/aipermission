import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet } from "../../lib/api";
import { VaultSessionDialog } from "./vault-session-dialog";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn() }));

beforeEach(() => {
  apiGet.mockReset();
});

it("selects an item beyond the first page and preserves it across searches", async () => {
  const user = userEvent.setup();
  const onStart = vi.fn();
  const first = Array.from({ length: 100 }, (_, index) => ({ id: index + 1, name: `KEY_${index + 1}`, owner_project_id: 4 }));
  apiGet.mockImplementation(async (path) => {
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
        options: { target_project_id: 4, projects: [{ id: 4, name: "My Project" }], defaults: [], items: first, total: 101 },
      }}
      onClose={vi.fn()}
      onStart={onStart}
    />,
  );

  await waitFor(() => expect(screen.getByRole("button", { name: "Load more" })).toBeVisible());
  await user.click(screen.getByRole("button", { name: "Load more" }));
  await user.click(await screen.findByRole("checkbox", { name: "Select KEY_101" }));
  await user.type(screen.getByPlaceholderText("Search this project"), "KEY_1");
  await waitFor(() => expect(apiGet.mock.calls.some(([path]) => String(path).includes("q=KEY_1"))).toBe(true));
  expect(screen.getByText("KEY_101")).toBeVisible();

  await user.click(screen.getByRole("button", { name: "Start session" }));
  expect(onStart).toHaveBeenCalledWith([{ item_id: 101, source_project_id: 4, replace_existing: false }]);
});
