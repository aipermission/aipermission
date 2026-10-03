import { act } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { deferredEditorMutation, renderEditor } from "../connector-editor-fixtures";

const target = { id: 8, connector_kind: "example", name: "My connector" };

it("serializes deletion and retains ownership through the post-commit refresh", async () => {
  const mutation = deferredEditorMutation();
  const refresh = deferredEditorMutation();
  const model = { deleteTarget: vi.fn(() => mutation.promise) };
  const { result, onRefresh } = renderEditor(model);
  onRefresh.mockReturnValue(refresh.promise);
  act(() => result.current.requestDelete(target));
  let pending!: Promise<boolean>;
  act(() => {
    const owner = result.current;
    pending = owner.remove(false);
    void owner.remove(true);
    owner.closeDelete();
    owner.requestDelete({ ...target, id: 9 });
    owner.openCreate();
    expect(owner.openEdit(target, null)).toBe(false);
    owner.closeEditor();
    owner.selectKind("other");
    owner.updateField("name", "late");
    void owner.save();
    owner.completeOperation(null, null);
  });
  expect(model.deleteTarget).toHaveBeenCalledOnce();
  expect(result.current.deleteDialog).toEqual({ open: true, target });
  expect(result.current.form.name).toBe("");
  await act(async () => mutation.resolve());
  expect(onRefresh).toHaveBeenCalledOnce();
  expect(result.current.actionState.state).toBe("deleting");
  act(() => result.current.closeDelete());
  expect(result.current.deleteDialog.open).toBe(true);
  await act(async () => {
    refresh.resolve();
    await pending;
  });
  expect(result.current.deleteDialog.open).toBe(false);
  expect(result.current.actionState).toEqual({ state: "idle", error: null, message: "Connector deleted." });
});

it("keeps a committed deletion distinct from a failed list refresh without replaying it", async () => {
  const model = { deleteTarget: vi.fn(async () => {}) };
  const { result, onRefresh } = renderEditor(model);
  onRefresh.mockRejectedValue(new Error("Refresh offline"));
  act(() => result.current.requestDelete(target));
  await act(async () => result.current.remove(false));
  expect(result.current.actionState.message).toBe("Connector deleted.");
  expect(result.current.actionState.error).toContain("Saved successfully");
  await act(async () => result.current.remove(false));
  expect(model.deleteTarget).toHaveBeenCalledOnce();
});

it("rejects a delayed delete handler after its reviewed target is replaced", async () => {
  const model = { deleteTarget: vi.fn(async () => {}) };
  const { result } = renderEditor(model);
  act(() => result.current.requestDelete(target));
  const stale = result.current.remove;
  act(() => result.current.requestDelete({ ...target, id: 9 }));
  await act(async () => expect(stale(false)).resolves.toBe(false));
  expect(model.deleteTarget).not.toHaveBeenCalled();
  expect(result.current.deleteDialog.target?.id).toBe(9);
});

it("does not refresh a deleted target after its editor unmounts before the response", async () => {
  const mutation = deferredEditorMutation();
  const { result, unmount, onRefresh } = renderEditor({ deleteTarget: vi.fn(() => mutation.promise) });
  act(() => result.current.requestDelete(target));
  let pending!: Promise<boolean>;
  act(() => {
    pending = result.current.remove(false);
  });
  unmount();
  await act(async () => {
    mutation.resolve();
    expect(await pending).toBe(false);
  });
  expect(onRefresh).not.toHaveBeenCalled();
});
