import { StrictMode } from "react";
import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { captureCredentialFamily } from "./capture-credential-family";
import { DeleteCredentialDialog } from "./credential-delete-dialog";
import type { CredentialFamilyDefinition } from "./credential-family-types";
import { renderCredentialFamily } from "../../test/render-credential-family";

const callbacks = vi.hoisted(() => ({ confirm: () => {}, close: () => {} }));
vi.mock("./credential-delete-dialog", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./credential-delete-dialog")>();
  return {
    ...actual,
    DeleteCredentialDialog: (props: Parameters<typeof DeleteCredentialDialog>[0]) => {
      callbacks.confirm = props.onDelete;
      callbacks.close = props.onClose;
      return <actual.DeleteCredentialDialog {...props} />;
    },
  };
});

type Row = { id: number; connector_kind: string; name: string };
const rows: Row[] = [
  { id: 1, connector_kind: "example", name: "A" },
  { id: 2, connector_kind: "example", name: "B" },
];

function deferred() {
  let resolve!: () => void;
  let reject!: (_error: Error) => void;
  const promise = new Promise<void>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

function renderFamily(refresh: () => Promise<void>, deleteCredential = vi.fn<() => Promise<void>>(async () => {})) {
  const definition: CredentialFamilyDefinition<{ name: string }, Row, object, "create"> = {
    kind: "example",
    label: "Example",
    decodeTargets: () => [],
    rows: () => rows,
    emptyState: () => ({ name: "" }),
    model: { deleteCredential, credentialStateFromRow: ({ row }) => ({ name: row.name }) },
    displayRow: (row) => ({
      row_id: String(row.id),
      connector_kind: row.connector_kind,
      connector_label: "Example",
      name: row.name,
      kind: "profile",
      target_label: "Target",
      metadata: [],
    }),
    renderForm: ({ editor }) => (
      <input
        aria-label="Draft name"
        value={editor.formState.name}
        onChange={(event) => editor.setFormState({ name: event.target.value })}
      />
    ),
  };
  const family = captureCredentialFamily(definition);
  // Replay mount effects as well as testing the normal interactive lifecycle.
  const Rows = family.Rows;
  const view = renderCredentialFamily(
    {
      ...family,
      Rows: (props) => (
        <StrictMode>
          <Rows {...props} />
        </StrictMode>
      ),
    },
    { refresh },
  );
  const user = userEvent.setup();
  async function openDelete(name: string) {
    const row = screen.getByText(name).closest("tr")!;
    await user.click(within(row).getByRole("button", { name: "Delete credential" }));
  }
  async function confirm() {
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete credential" }));
  }
  return { ...view, user, deleteCredential, openDelete, confirm };
}

it.each(["resolve", "reject"] as const)("does not close B's pending delete after A refresh %s", async (settlement) => {
  const oldRefresh = deferred();
  const newDelete = deferred();
  const refresh = vi.fn<() => Promise<void>>().mockReturnValueOnce(oldRefresh.promise).mockResolvedValue(undefined);
  const model = vi.fn<() => Promise<void>>().mockResolvedValueOnce(undefined).mockReturnValueOnce(newDelete.promise);
  const view = renderFamily(refresh, model);
  await view.openDelete("A");
  await view.confirm();
  expect(refresh).toHaveBeenCalledOnce();
  await view.user.click(screen.getByRole("button", { name: "Cancel" }));
  await view.openDelete("B");
  await view.confirm();
  await act(async () => {
    if (settlement === "resolve") oldRefresh.resolve();
    else oldRefresh.reject(new Error("old refresh failed"));
  });
  expect(screen.getByRole("dialog", { name: "Delete B" })).toBeVisible();
  expect(screen.getByRole("button", { name: "Deleting..." })).toBeDisabled();
  expect(view.onStateChange).toHaveBeenLastCalledWith("example", { state: "deleting", error: null, message: null });
  await act(async () => newDelete.resolve());
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(refresh).toHaveBeenCalledTimes(2);
});

it.each([
  ["resolve", "A"],
  ["reject", "A"],
  ["resolve", "B"],
  ["reject", "B"],
] as const)("preserves an opened dialog after A refresh %s for %s (including ABA)", async (settlement, replacement) => {
  const oldRefresh = deferred();
  const view = renderFamily(() => oldRefresh.promise);
  await view.openDelete("A");
  await view.confirm();
  await view.user.click(screen.getByRole("button", { name: "Cancel" }));
  await view.openDelete("B");
  if (replacement === "A") {
    await view.user.click(screen.getByRole("button", { name: "Cancel" }));
    await view.openDelete("A");
  }
  await act(async () => {
    if (settlement === "resolve") oldRefresh.resolve();
    else oldRefresh.reject(new Error("old refresh failed"));
  });
  expect(screen.getByRole("dialog", { name: `Delete ${replacement}` })).toBeVisible();
  expect(screen.getByRole("button", { name: "Delete credential" })).toBeEnabled();
  expect(view.onStateChange).toHaveBeenLastCalledWith("example", { state: "idle", error: null, message: null });
});

it.each(["resolve", "reject"] as const)("ignores retired refresh %s and callbacks after same-kind remount", async (settlement) => {
  const refresh = deferred();
  const old = renderFamily(() => refresh.promise);
  await old.openDelete("A");
  await old.confirm();
  const oldConfirm = callbacks.confirm;
  const oldClose = callbacks.close;
  old.unmount();
  const publications = old.onStateChange.mock.calls.length;
  const current = renderFamily(async () => {});
  await current.openDelete("B");
  act(() => {
    oldConfirm();
    oldClose();
  });
  await act(async () => {
    if (settlement === "resolve") refresh.resolve();
    else refresh.reject(new Error("retired refresh failed"));
  });
  expect(screen.getByRole("dialog", { name: "Delete B" })).toBeVisible();
  expect(old.onStateChange).toHaveBeenCalledTimes(publications);
  expect(old.deleteCredential).toHaveBeenCalledOnce();
  expect(current.deleteCredential).not.toHaveBeenCalled();
  await current.confirm();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(current.deleteCredential).toHaveBeenCalledExactlyOnceWith({ row: rows[1] });
});

it.each(["create", "edit"] as const)("preserves a replacement %s draft after A refresh failure", async (mode) => {
  const oldRefresh = deferred();
  const view = renderFamily(() => oldRefresh.promise);
  await view.openDelete("A");
  await view.confirm();
  if (mode === "create") act(() => view.openCreate());
  else {
    await view.user.click(screen.getByRole("button", { name: "Cancel" }));
    await view.user.click(within(screen.getByText("B").closest("tr")!).getByRole("button", { name: "Edit credential" }));
  }
  await view.user.clear(screen.getByLabelText("Draft name"));
  await view.user.type(screen.getByLabelText("Draft name"), "replacement");
  await act(async () => oldRefresh.reject(new Error("old refresh failed")));
  expect(screen.getByRole("dialog", { name: `${mode === "create" ? "Add" : "Edit"} Example credential` })).toBeVisible();
  expect(screen.getByLabelText("Draft name")).toHaveValue("replacement");
  expect(view.onStateChange).toHaveBeenLastCalledWith("example", { state: "idle", error: null, message: null });
});

it("ignores retained dialog callbacks and claims duplicate confirms before a render", async () => {
  const api = deferred();
  const refresh = deferred();
  const view = renderFamily(
    () => refresh.promise,
    vi.fn(() => api.promise),
  );
  await view.openDelete("A");
  const oldConfirm = callbacks.confirm;
  const oldClose = callbacks.close;
  await view.user.click(screen.getByRole("button", { name: "Cancel" }));
  await view.openDelete("B");
  act(() => {
    oldConfirm();
    oldClose();
  });
  expect(view.deleteCredential).not.toHaveBeenCalled();
  expect(screen.getByRole("dialog", { name: "Delete B" })).toBeVisible();
  act(() => {
    callbacks.confirm();
    callbacks.confirm();
  });
  expect(view.deleteCredential).toHaveBeenCalledExactlyOnceWith({ row: rows[1] });
  await act(async () => api.resolve());
  act(() => {
    callbacks.confirm();
  });
  expect(view.deleteCredential).toHaveBeenCalledOnce();
  await act(async () => refresh.resolve());
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it.each(["resolve", "reject"] as const)("closes the same-owner dialog after refresh %s", async (settlement) => {
  const pending = deferred();
  const view = renderFamily(() => pending.promise);
  await view.openDelete("A");
  await view.confirm();
  expect(screen.getByRole("dialog", { name: "Delete A" })).toBeVisible();
  await act(async () => {
    if (settlement === "resolve") pending.resolve();
    else pending.reject(new Error("offline"));
  });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(view.onStateChange).toHaveBeenLastCalledWith("example", {
    state: "idle",
    message: "Credential deleted.",
    error: settlement === "resolve" ? null : "Saved successfully, but the list refresh failed: offline",
  });
});

it("keeps API failures in the owning dialog and supports retry", async () => {
  const refresh = vi.fn(async () => {});
  const view = renderFamily(
    refresh,
    vi.fn<() => Promise<void>>().mockRejectedValueOnce(new Error("delete failed")).mockResolvedValue(undefined),
  );
  await view.openDelete("A");
  await view.confirm();
  expect(screen.getByRole("dialog", { name: "Delete A" })).toBeVisible();
  expect(screen.getByText("delete failed")).toBeVisible();
  expect(refresh).not.toHaveBeenCalled();
  await view.confirm();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(refresh).toHaveBeenCalledOnce();
});
