import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../lib/api";
import { filterVaultItemsByExpiry, useVaultCollection } from "./use-vault-collection";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn(), apiPut: vi.fn() }));

function CollectionHarness() {
  const vault = useVaultCollection();
  return (
    <div>
      <p data-testid="items">{vault.items.data.map((item) => item.name).join(",")}</p>
      <p data-testid="project">{vault.projects.data[0]?.name || ""}</p>
      <p data-testid="editor">{`${vault.editor.open}:${vault.editor.owner_project_id}:${vault.editor.name}`}</p>
      <p data-testid="action">{vault.action.message || vault.action.error}</p>
      <button type="button" onClick={vault.openCreate}>
        Open
      </button>
      <button type="button" onClick={vault.closeEditor}>
        Close
      </button>
      <button type="button" onClick={() => vault.setEditor((current) => ({ ...current, name: "my_key", value: "secret" }))}>
        Fill
      </button>
      <button type="button" onClick={(event) => void vault.saveItem(event)}>
        Save
      </button>
    </div>
  );
}

beforeEach(() => {
  apiGet.mockReset();
  apiPost.mockReset();
  apiGet.mockImplementation((path) => {
    if (path === "/api/projects") return Promise.resolve({ items: [{ id: 4, name: "My Project", slug: "my-project" }] });
    if (path.startsWith("/api/vault-items")) return Promise.resolve({ items: [{ id: 1, name: "KEY" }], total: 1 });
    throw new Error(`Unexpected path ${path}`);
  });
  apiPost.mockResolvedValue({ id: 2 });
});

it("owns debounced Vault listing and create lifecycle", async () => {
  const user = userEvent.setup();
  render(<CollectionHarness />);
  expect(await screen.findByTestId("project")).toHaveTextContent("My Project");
  await waitFor(() => expect(screen.getByTestId("items")).toHaveTextContent("KEY"));
  await user.click(screen.getByRole("button", { name: "Open" }));
  expect(screen.getByTestId("editor")).toHaveTextContent("true:4");
  await user.click(screen.getByRole("button", { name: "Fill" }));
  await user.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.getByTestId("action")).toHaveTextContent("Vault item created"));
  expect(apiPost).toHaveBeenCalledWith(
    "/api/vault-items",
    expect.objectContaining({ name: "MY_KEY", value: "secret", owner_project_id: 4 }),
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
});

it("filters expiry views at a stable point in time", () => {
  const now = Date.parse("2026-09-07T10:00:00Z");
  const items = [
    { id: 1, expires_at: "2026-09-06T10:00:00Z" },
    { id: 2, expires_at: "2026-09-10T10:00:00Z", expiry_warning_days: 7 },
    { id: 3, expires_at: null },
  ];
  expect(filterVaultItemsByExpiry(items, "expired", now).map((item) => item.id)).toEqual([1]);
  expect(filterVaultItemsByExpiry(items, "warning", now).map((item) => item.id)).toEqual([2]);
  expect(filterVaultItemsByExpiry(items, "none", now).map((item) => item.id)).toEqual([3]);
});

it("does not let a late create close a newly opened editor", async () => {
  const user = userEvent.setup();
  let resolveSave;
  const save = new Promise((resolve) => {
    resolveSave = resolve;
  });
  apiPost.mockReturnValue(save);
  render(<CollectionHarness />);
  expect(await screen.findByTestId("project")).toHaveTextContent("My Project");

  await user.click(screen.getByRole("button", { name: "Open" }));
  await user.click(screen.getByRole("button", { name: "Fill" }));
  await user.click(screen.getByRole("button", { name: "Save" }));
  const signal = apiPost.mock.calls[0][2].signal;
  await user.click(screen.getByRole("button", { name: "Close" }));
  await user.click(screen.getByRole("button", { name: "Open" }));
  expect(signal.aborted).toBe(true);
  expect(screen.getByTestId("editor")).toHaveTextContent("true:4:");

  await act(async () => resolveSave({ id: 2 }));
  expect(screen.getByTestId("editor")).toHaveTextContent("true:4:");
  expect(screen.getByTestId("action")).toHaveTextContent("");
});
