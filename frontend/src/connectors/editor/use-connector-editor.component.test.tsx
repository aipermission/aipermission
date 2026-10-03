import { act } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { deferredEditorMutation as deferred, renderEditor } from "../../test/connector-editor-fixtures";
import type { SyncContext } from "../../test/connector-editor-fixtures";

describe("useConnectorEditor", () => {
  it("saves through the connector model and clears sensitive form state", async () => {
    const model = { save: vi.fn(async () => {}), syncForm: ({ form }: SyncContext) => form };
    const { result, onRefresh } = renderEditor(model);

    act(() => result.current.openCreate("example"));
    act(() => result.current.updateField("name", "Production"));

    await act(async () => result.current.save({ preventDefault: vi.fn() }));

    expect(model.save).toHaveBeenCalledWith({
      mode: "create",
      form: { connector_kind: "example", name: "Production", project_id: "2" },
      target: null,
    });
    expect(onRefresh).toHaveBeenCalledOnce();
    expect(result.current.drawer.open).toBe(false);
    expect(result.current.form.name).toBe("");
    expect(result.current.actionState.message).toBe("Connector created.");
  });

  it("keeps the editor open after an API failure and supports retry", async () => {
    const model = {
      save: vi.fn().mockRejectedValueOnce(new Error("API unavailable")).mockResolvedValueOnce(undefined),
      syncForm: ({ form }: SyncContext) => form,
    };
    const { result } = renderEditor(model);
    act(() => result.current.openCreate("example"));
    act(() => result.current.updateField("name", "Retry target"));

    await act(async () => result.current.save({ preventDefault() {} }));
    expect(result.current.drawer.open).toBe(true);
    expect(result.current.actionState).toEqual({ state: "error", error: "API unavailable", message: null });

    await act(async () => result.current.save({ preventDefault() {} }));
    expect(model.save).toHaveBeenCalledTimes(2);
    expect(result.current.drawer.open).toBe(false);
    expect(result.current.actionState.message).toBe("Connector created.");
  });

  it("surfaces missing model validation and resets state on cancel", async () => {
    const { result } = renderEditor(null);
    act(() => result.current.openCreate("missing"));
    act(() => result.current.updateField("name", "Unsaved"));

    await act(async () => result.current.save({ preventDefault() {} }));
    expect(result.current.actionState.error).toBe("Connector model not found for missing.");

    act(() => result.current.closeEditor());
    expect(result.current.drawer.open).toBe(false);
    expect(result.current.actionState.state).toBe("idle");
  });

  it("opens profile-bound target edits only with a selected profile", () => {
    const target = { id: 8, connector_kind: "example", project_id: "5", profiles: [{ id: 7 }] };
    const profile = { id: 7 };
    const model = {
      formFromTarget: vi.fn(() => ({ connector_kind: "example", name: "Existing" })),
      syncForm: ({ form }: SyncContext) => form,
    };
    const { result } = renderEditor(model);

    act(() => expect(result.current.openEdit(target, null)).toBe(false));
    expect(result.current.actionState.error).toMatch(/Select a credential profile/);
    expect(result.current.drawer.open).toBe(false);

    act(() => expect(result.current.openEdit(target, profile)).toBe(true));
    expect(model.formFromTarget).toHaveBeenCalledWith({ target, profile });
    expect(result.current.form).toEqual({ connector_kind: "example", name: "Existing", project_id: "5" });
    expect(result.current.drawer).toEqual({ open: true, mode: "edit", kind: "example", target });
    expect(result.current.actionState.state).toBe("idle");
  });

  it("does not report a persisted connector as a save failure when refresh fails", async () => {
    const model = { save: vi.fn(async () => undefined), syncForm: ({ form }: SyncContext) => form };
    const { result, onRefresh } = renderEditor(model);
    onRefresh.mockRejectedValueOnce(new Error("refresh unavailable"));
    act(() => result.current.openCreate("example"));

    await act(async () => result.current.save({ preventDefault() {} }));

    expect(result.current.drawer.open).toBe(false);
    expect(result.current.actionState.state).toBe("idle");
    expect(result.current.actionState.message).toBe("Connector created.");
    expect(result.current.actionState.error).toMatch(/Saved successfully.*refresh unavailable/);
  });

  it("does not let a retired save close or reset a replacement draft", async () => {
    let resolveSave!: () => void;
    const pendingSave = new Promise<void>((resolve) => {
      resolveSave = resolve;
    });
    const model = { save: vi.fn(() => pendingSave), syncForm: ({ form }: SyncContext) => form };
    const { result, onRefresh } = renderEditor(model);

    act(() => result.current.openCreate("example"));
    act(() => result.current.updateField("name", "Old draft"));
    let save: Promise<boolean> | undefined;
    act(() => {
      save = result.current.save({ preventDefault() {} });
    });
    act(() => result.current.closeEditor());
    act(() => result.current.openCreate("example"));
    act(() => result.current.updateField("name", "Replacement draft"));

    await act(async () => resolveSave());

    await expect(save).resolves.toBe(false);
    expect(result.current.drawer.open).toBe(true);
    expect(result.current.form.name).toBe("Replacement draft");
    expect(result.current.actionState.state).toBe("idle");
    expect(onRefresh).not.toHaveBeenCalled();
  });

  it("locks fields and duplicate submits until the same connector save settles", async () => {
    const pending = deferred();
    const model = {
      save: vi.fn(() => pending.promise),
      syncForm: ({ form, firstCredentialID }: SyncContext) => ({ ...form, credential_id: firstCredentialID }),
    };
    const { result, rerender } = renderEditor(model);
    act(() => result.current.openCreate("example"));
    act(() => result.current.updateField("name", "original"));
    act(() => result.current.updateField("credential_id", "4"));
    let firstSave: Promise<boolean> | undefined;
    let secondSave: Promise<boolean> | undefined;
    act(() => {
      firstSave = result.current.save({ preventDefault() {} });
      result.current.updateField("name", "broader scope");
      result.current.selectKind("other");
      secondSave = result.current.save({ preventDefault() {} });
    });
    rerender({ firstCredentialID: "5" });

    expect(result.current.form.name).toBe("original");
    expect(result.current.form.connector_kind).toBe("example");
    expect(result.current.form.credential_id).toBe("4");
    expect(model.save).toHaveBeenCalledTimes(1);
    await act(async () => pending.resolve());
    await expect(firstSave).resolves.toBe(true);
    await expect(secondSave).resolves.toBe(false);
  });

  it("ignores delayed field changes from a retired connector editor", () => {
    const model = { syncForm: ({ form }: SyncContext) => form };
    const { result } = renderEditor(model);
    act(() => result.current.openCreate("example"));
    const staleUpdateField = result.current.updateField;
    act(() => result.current.closeEditor());
    act(() => result.current.openCreate("example"));
    act(() => result.current.updateField("name", "replacement"));
    act(() => staleUpdateField("name", "old"));
    expect(result.current.form.name).toBe("replacement");
  });

  it("keeps cleared connector fields empty after a successful save", async () => {
    const model = { save: vi.fn(async () => undefined), syncForm: ({ form }: SyncContext) => form };
    const { result } = renderEditor(model);
    act(() => result.current.openCreate("example"));
    act(() => result.current.updateField("name", "secret-target"));
    const staleUpdateField = result.current.updateField;

    await act(async () => result.current.save({ preventDefault() {} }));
    act(() => staleUpdateField("name", "secret-target"));

    expect(result.current.form.name).toBe("");
  });

  it("hands connector-owned recovery operations back to the route", async () => {
    const recovery = { open: true, connector_kind: "example", type: "trust" };
    const model = {
      save: vi.fn(async () => Promise.reject(new Error("Trust required"))),
      syncForm: ({ form }: SyncContext) => form,
      operationFromError: vi.fn(() => recovery),
    };
    const { result, onOperation } = renderEditor(model);
    act(() => result.current.openCreate("example"));

    await act(async () => result.current.save({ preventDefault() {} }));

    expect(onOperation).toHaveBeenCalledWith(recovery);
    expect(result.current.drawer.open).toBe(true);
    expect(result.current.actionState.state).toBe("idle");
  });

  it("keeps destructive dialog state explicit and clears it after deletion", async () => {
    const target = { id: 8, connector_kind: "example", name: "Old target" };
    const model = { deleteTarget: vi.fn(async () => {}), syncForm: ({ form }: SyncContext) => form };
    const { result, onRefresh } = renderEditor(model);

    act(() => result.current.requestDelete(target));
    expect(result.current.deleteDialog).toEqual({ open: true, target });

    await act(async () => result.current.remove(false));

    expect(model.deleteTarget).toHaveBeenCalledWith({ target, removeKey: false });
    expect(result.current.deleteDialog).toEqual({ open: false, target: null });
    expect(result.current.actionState.message).toBe("Connector deleted.");
    expect(onRefresh).toHaveBeenCalledOnce();
  });

  it.each(["success", "failure"])("keeps an owned %s delete visible despite replacement attempts", async (outcome) => {
    const pendingDelete = deferred();
    const first = { id: 8, connector_kind: "example", name: "First target" };
    const replacement = { id: 9, connector_kind: "example", name: "Replacement target" };
    const model = { deleteTarget: vi.fn(() => pendingDelete.promise), syncForm: ({ form }: SyncContext) => form };
    const { result, onRefresh } = renderEditor(model);

    act(() => result.current.requestDelete(first));
    let removal: Promise<boolean> | undefined;
    act(() => {
      removal = result.current.remove(false);
    });
    act(() => result.current.closeDelete());
    act(() => result.current.requestDelete(replacement));
    expect(result.current.deleteDialog).toEqual({ open: true, target: first });
    expect(result.current.actionState.state).toBe("deleting");

    await act(async () => {
      if (outcome === "success") pendingDelete.resolve();
      else pendingDelete.reject(new Error("retired delete failed"));
    });

    await expect(removal).resolves.toBe(outcome === "success");
    expect(result.current.deleteDialog).toEqual(outcome === "success" ? { open: false, target: null } : { open: true, target: first });
    expect(result.current.actionState.state).toBe(outcome === "success" ? "idle" : "error");
    expect(onRefresh).toHaveBeenCalledTimes(outcome === "success" ? 1 : 0);
  });
});
