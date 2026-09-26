import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet as realGet, apiPost as realPost, apiPut as realPut } from "../../lib/api";
import { useVaultBindings, vaultSessionTargets } from "./use-vault-bindings";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn(), apiPut: vi.fn() }));
const apiGet = vi.mocked(realGet);
const apiPost = vi.mocked(realPost);
const apiPut = vi.mocked(realPut);

function signalFromOptions(options: unknown): AbortSignal {
  if (!options || typeof options !== "object" || !("signal" in options) || !(options.signal instanceof AbortSignal)) {
    throw new Error("Missing binding request signal");
  }
  return options.signal;
}

function deferred() {
  let resolve!: (_value: unknown) => void;
  const promise = new Promise<unknown>((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

function BindingsHarness({ setAction = vi.fn() }: Partial<Parameters<typeof useVaultBindings>[0]>) {
  const owner = useVaultBindings({ setAction });
  const itemA = { id: 5, name: "ITEM_A", owner_project_id: 2 };
  const itemB = { id: 6, name: "ITEM_B", owner_project_id: 3 };
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
      <button type="button" disabled={!owner.bindings.data[1]} onClick={() => void owner.deleteBinding(owner.bindings.data[1])}>
        Delete second
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
            connector_kind: "fixture",
            profiles: [
              { id: 8, label: "Supported", vault_session_supported: true },
              { id: 9, label: "Disabled", vault_session_supported: false },
            ],
          },
          { id: 10, name: "Hidden", connector_kind: "fixture", profiles: [{ id: 11, label: "Hidden", vault_session_supported: false }] },
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
  const signals = apiGet.mock.calls.map((call) => signalFromOptions(call[1]));
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
  const signal = signalFromOptions(apiPut.mock.calls[0]?.[2]);

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
    Promise.resolve(path === "/api/connector-targets/inventory" ? { items: [] } : { items: [{ id: 12, vault_item_id: 5, binding_revision: 4,
      source_project_id: 2, target_id: 7, profile_id: 8, replace_existing: false, target_name: "Target", profile_label: "main",
      source_project_name: "My Project", connector_kind: "fixture" }] }),
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

it("does not make malformed current target capabilities selectable", async () => {
  const user = userEvent.setup();
  apiGet.mockImplementation((path) => Promise.resolve(path === "/api/connector-targets/inventory"
    ? { items: [{ id: 7, name: "Target", connector_kind: "fixture", profiles: [{ id: 8, label: "main", vault_session_supported: "true" }] }] }
    : { items: [] }));
  render(<BindingsHarness />);
  await user.click(screen.getByRole("button", { name: "Open A" }));
  await waitFor(() => expect(screen.getByTestId("state")).toHaveTextContent("error"));
  expect(screen.getByTestId("targets")).toBeEmptyDOMElement();
});

it("rejects a binding list for another Vault item instead of applying its revisions", async () => {
  const user = userEvent.setup();
  apiGet.mockImplementation((path) => Promise.resolve(path === "/api/connector-targets/inventory"
    ? { items: [] } : { items: [{ id: 1, vault_item_id: 6 }] }));
  render(<BindingsHarness />);
  await user.click(screen.getByRole("button", { name: "Open A" }));
  await waitFor(() => expect(screen.getByTestId("state")).toHaveTextContent("error"));
  expect(screen.getByTestId("bindings")).toBeEmptyDOMElement();
});

it("serializes deletions and saves without invalidating the active mutation", async () => {
  const user = userEvent.setup();
  const deletion = deferred();
  const item = { vault_item_id: 5, binding_revision: 4, source_project_id: 2, target_id: 7, profile_id: 8,
    replace_existing: false, target_name: "Target", profile_label: "main", source_project_name: "My Project", connector_kind: "fixture" };
  apiGet.mockImplementation((path) => Promise.resolve(path === "/api/connector-targets/inventory" ? { items: [] }
    : { items: [{ ...item, id: 12 }, { ...item, id: 13, profile_id: 9 }] }));
  apiPost.mockReturnValue(deletion.promise);
  render(<BindingsHarness />);
  await user.click(screen.getByRole("button", { name: "Open A" }));
  await waitFor(() => expect(screen.getByTestId("bindings")).toHaveTextContent("12,13"));
  await user.click(screen.getByRole("button", { name: "Delete" }));
  const signal = signalFromOptions(apiPost.mock.calls[0]?.[2]);
  await user.click(screen.getByRole("button", { name: "Delete second" }));
  await user.click(screen.getByRole("button", { name: "Save" }));
  expect(apiPost).toHaveBeenCalledOnce();
  expect(apiPut).not.toHaveBeenCalled();
  expect(signal.aborted).toBe(false);
  deletion.resolve({});
  await waitFor(() => expect(screen.getByTestId("bindings")).toHaveTextContent(/^13$/));
  await user.click(screen.getByRole("button", { name: "Delete" }));
  await waitFor(() => expect(screen.getByTestId("bindings")).toBeEmptyDOMElement());
  expect(apiPost).toHaveBeenCalledTimes(2);
});
