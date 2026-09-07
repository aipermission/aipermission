import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost, apiPut } from "../../lib/api";
import { filterVaultItemsByExpiry, useVaultCollection } from "./use-vault-collection";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn(), apiPut: vi.fn() }));

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((next, failure) => {
    resolve = next;
    reject = failure;
  });
  return { promise, resolve, reject };
}

function CollectionHarness() {
  const vault = useVaultCollection();
  return (
    <div>
      <p data-testid="items">{vault.items.data.map((item) => item.name).join(",")}</p>
      <p data-testid="project">{vault.projects.data[0]?.name || ""}</p>
      <p data-testid="query">{vault.filters.query}</p>
      <p data-testid="items-state">{`${vault.items.state}:${vault.items.error || ""}`}</p>
      <p data-testid="editor">{`${vault.editor.open}:${vault.editor.owner_project_id}:${vault.editor.name}`}</p>
      <p data-testid="action">{vault.action.message || vault.action.error}</p>
      <button type="button" onClick={vault.openCreate}>
        Open
      </button>
      <button
        type="button"
        onClick={() =>
          vault.openEdit({
            id: 7,
            source: "imported",
            name: "existing_key",
            owner_project_id: 4,
            project_ids: [],
            secret_type: "generic_secret",
            metadata_revision: 3,
          })
        }
      >
        Edit
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
      <button type="button" onClick={() => vault.setFilters({ query: "current" })}>
        Filter
      </button>
    </div>
  );
}

beforeEach(() => {
  apiGet.mockReset();
  apiPost.mockReset();
  apiPut.mockReset();
  apiGet.mockImplementation((path) => {
    if (path === "/api/projects") return Promise.resolve({ items: [{ id: 4, name: "My Project", slug: "my-project" }] });
    if (path.startsWith("/api/vault-items")) return Promise.resolve({ items: [{ id: 1, name: "KEY" }], total: 1 });
    throw new Error(`Unexpected path ${path}`);
  });
  apiPost.mockResolvedValue({ id: 2 });
  apiPut.mockResolvedValue({ id: 7 });
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

it("updates Vault metadata without replacing the existing value", async () => {
  const user = userEvent.setup();
  render(<CollectionHarness />);
  expect(await screen.findByTestId("project")).toHaveTextContent("My Project");

  await user.click(screen.getByRole("button", { name: "Edit" }));
  expect(screen.getByTestId("editor")).toHaveTextContent("true:4:existing_key");
  await user.click(screen.getByRole("button", { name: "Save" }));

  await waitFor(() => expect(screen.getByTestId("action")).toHaveTextContent("Vault item updated"));
  expect(apiPut).toHaveBeenCalledWith(
    "/api/vault-items/7",
    expect.objectContaining({ name: "EXISTING_KEY", expected_metadata_revision: 3 }),
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
});

it.each([
  ["success", (pending) => pending.resolve({ items: [{ id: 9, name: "STALE" }], total: 1 })],
  ["error", (pending) => pending.reject(new Error("stale failure"))],
])("invalidates a stale Vault list %s as soon as filters change", async (_outcome, settle) => {
  const user = userEvent.setup();
  const pending = deferred();
  apiGet.mockImplementation((path) => {
    if (path === "/api/projects") return Promise.resolve({ items: [{ id: 4, name: "My Project", slug: "my-project" }] });
    if (path === "/api/vault-items") return pending.promise;
    if (path === "/api/vault-items?q=current") return Promise.resolve({ items: [{ id: 10, name: "CURRENT" }], total: 1 });
    throw new Error(`Unexpected path ${path}`);
  });

  render(<CollectionHarness />);
  await waitFor(() =>
    expect(apiGet).toHaveBeenCalledWith("/api/vault-items", expect.objectContaining({ signal: expect.any(AbortSignal) })),
  );
  const staleSignal = apiGet.mock.calls.find(([path]) => path === "/api/vault-items")[1].signal;

  await user.click(screen.getByRole("button", { name: "Filter" }));
  expect(screen.getByTestId("query")).toHaveTextContent("current");
  expect(staleSignal.aborted).toBe(true);
  settle(pending);

  await waitFor(() => expect(screen.getByTestId("items")).toHaveTextContent("CURRENT"));
  expect(screen.getByTestId("items")).not.toHaveTextContent("STALE");
  expect(screen.getByTestId("items-state")).not.toHaveTextContent("stale failure");
});
