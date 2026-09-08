import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost, apiPut } from "../../lib/api";
import { useVaultBindings, vaultSessionTargets } from "./use-vault-bindings";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn(), apiPut: vi.fn() }));

function deferred() {
  let resolve;
  const promise = new Promise((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

function BindingsHarness({ setAction = vi.fn() }) {
  const owner = useVaultBindings({ setAction });
  const itemA = { id: 5, owner_project_id: 2 };
  const itemB = { id: 6, owner_project_id: 3 };
  return (
    <div>
      <p data-testid="state">{owner.bindings.state}</p>
      <p data-testid="item">{owner.bindings.item?.id || "none"}</p>
      <p data-testid="targets">{owner.bindings.targets.map((target) => target.name).join(",")}</p>
      <p data-testid="bindings">{owner.bindings.data.map((binding) => binding.id).join(",")}</p>
      <button type="button" onClick={() => void owner.openBindings(itemA)}>
        Open A
      </button>
      <button type="button" onClick={() => void owner.openBindings(itemB)}>
        Open B
      </button>
      <button type="button" onClick={owner.closeBindings}>
        Close
      </button>
      <button
        type="button"
        onClick={() => owner.setBindings((current) => ({ ...current, target_id: "7", profile_id: "8", source_project_id: "2" }))}
      >
        Select
      </button>
      <button type="button" onClick={(event) => void owner.saveBinding(event)}>
        Save
      </button>
      <button type="button" disabled={!owner.bindings.data[0]} onClick={() => void owner.deleteBinding(owner.bindings.data[0])}>
        Delete
      </button>
    </div>
  );
}

beforeEach(() => {
  apiGet.mockReset();
  apiPost.mockReset();
  apiPut.mockReset();
});

it("loads only targets with Vault-capable profiles and saves optimistic bindings", async () => {
  const user = userEvent.setup();
  apiGet.mockImplementation((path) => {
    if (path === "/api/connector-targets/inventory") {
      return Promise.resolve({
        items: [
          {
            id: 7,
            name: "Supported",
            profiles: [
              { id: 8, vault_session_supported: true },
              { id: 9, vault_session_supported: false },
            ],
          },
          { id: 10, name: "Hidden", profiles: [{ id: 11, vault_session_supported: false }] },
        ],
      });
    }
    return Promise.resolve({ items: [] });
  });
  apiPut.mockResolvedValue({});
  render(<BindingsHarness />);
  await user.click(screen.getByRole("button", { name: "Open A" }));
  expect(await screen.findByTestId("targets")).toHaveTextContent("Supported");
  await user.click(screen.getByRole("button", { name: "Select" }));
  await user.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.getByTestId("state")).toHaveTextContent("ready"));
  expect(apiPut).toHaveBeenCalledWith(
    "/api/vault-default-bindings",
    expect.objectContaining({ vault_item_id: 5, source_project_id: 2, target_id: 7, profile_id: 8, expected_binding_revision: 0 }),
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
});

it("filters every unsupported target profile", () => {
  expect(vaultSessionTargets([{ id: 1, profiles: [{ vault_session_supported: false }] }])).toEqual([]);
});

it("cancels both pending binding reads when the dialog closes", async () => {
  const user = userEvent.setup();
  const bindings = deferred();
  const inventory = deferred();
  apiGet.mockImplementation((path) => (path === "/api/connector-targets/inventory" ? inventory.promise : bindings.promise));
  render(<BindingsHarness />);
  await user.click(screen.getByRole("button", { name: "Open A" }));
  const signals = apiGet.mock.calls.map((call) => call[1].signal);
  await user.click(screen.getByRole("button", { name: "Close" }));
  expect(signals).toHaveLength(2);
  expect(signals.every((signal) => signal.aborted)).toBe(true);
  bindings.resolve({ items: [{ id: 1 }] });
  inventory.resolve({ items: [] });
  await waitFor(() => expect(screen.getByTestId("state")).toHaveTextContent("idle"));
});

it("does not let a late save overwrite a newly opened binding dialog", async () => {
  const user = userEvent.setup();
  const save = deferred();
  const setAction = vi.fn();
  apiGet.mockResolvedValue({ items: [] });
  apiPut.mockReturnValue(save.promise);
  render(<BindingsHarness setAction={setAction} />);

  await user.click(screen.getByRole("button", { name: "Open A" }));
  await waitFor(() => expect(screen.getByTestId("state")).toHaveTextContent("ready"));
  await user.click(screen.getByRole("button", { name: "Select" }));
  await user.click(screen.getByRole("button", { name: "Save" }));
  const signal = apiPut.mock.calls[0][2].signal;

  await user.click(screen.getByRole("button", { name: "Close" }));
  await user.click(screen.getByRole("button", { name: "Open B" }));
  await waitFor(() => expect(screen.getByTestId("item")).toHaveTextContent("6"));
  expect(signal.aborted).toBe(true);
  save.resolve({});

  await waitFor(() => expect(screen.getByTestId("state")).toHaveTextContent("ready"));
  expect(screen.getByTestId("item")).toHaveTextContent("6");
  expect(setAction).not.toHaveBeenCalled();
});

it("removes the selected binding with its revision", async () => {
  const user = userEvent.setup();
  const setAction = vi.fn();
  apiGet.mockImplementation((path) =>
    Promise.resolve(path === "/api/connector-targets/inventory" ? { items: [] } : { items: [{ id: 12, binding_revision: 4 }] }),
  );
  apiPost.mockResolvedValue({});
  render(<BindingsHarness setAction={setAction} />);

  await user.click(screen.getByRole("button", { name: "Open A" }));
  expect(await screen.findByTestId("bindings")).toHaveTextContent("12");
  await user.click(screen.getByRole("button", { name: "Delete" }));

  await waitFor(() => expect(screen.getByTestId("bindings")).toHaveTextContent(""));
  expect(apiPost).toHaveBeenCalledWith(
    "/api/vault-default-bindings/12/delete",
    { expected_binding_revision: 4 },
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
  expect(setAction).toHaveBeenCalledWith(expect.objectContaining({ message: "Default session environment binding removed." }));
});

it("keeps the current dialog open when saving fails", async () => {
  const user = userEvent.setup();
  apiGet.mockResolvedValue({ items: [] });
  apiPut.mockRejectedValue(new Error("binding conflict"));
  render(<BindingsHarness />);

  await user.click(screen.getByRole("button", { name: "Open A" }));
  await user.click(screen.getByRole("button", { name: "Select" }));
  await user.click(screen.getByRole("button", { name: "Save" }));

  await waitFor(() => expect(screen.getByTestId("state")).toHaveTextContent("error"));
  expect(screen.getByTestId("item")).toHaveTextContent("5");
});
