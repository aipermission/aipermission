import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useCredentialProfileEditor } from "./use-credential-profile-editor";

const emptyState = (kind) => ({ form: { connector_kind: kind, label: "", password: "" } });

function renderEditor(model) {
  const onRefresh = vi.fn(async () => {});
  const hook = renderHook(() =>
    useCredentialProfileEditor({
      defaultKind: "example",
      targets: [{ id: 4, connector_kind: "example" }],
      emptyStateForKind: emptyState,
      modelForKind: () => model,
      onRefresh,
    }),
  );
  return { ...hook, onRefresh };
}

describe("useCredentialProfileEditor", () => {
  it("saves through the connector model and clears sensitive form state", async () => {
    const model = { saveCredential: vi.fn(async () => ({ message: "Profile created." })) };
    const { result, onRefresh } = renderEditor(model);
    act(() => result.current.openCreate("example"));
    act(() => result.current.setFormState({ form: { connector_kind: "example", label: "readonly", password: "secret" } }));

    await act(async () => result.current.save({ preventDefault() {} }, "create"));

    expect(model.saveCredential).toHaveBeenCalledWith({
      operation: "create",
      row: null,
      formState: { form: { connector_kind: "example", label: "readonly", password: "secret" } },
      targets: [{ id: 4, connector_kind: "example" }],
    });
    expect(onRefresh).toHaveBeenCalledOnce();
    expect(result.current.drawer.open).toBe(false);
    expect(result.current.formState.form.password).toBe("");
    expect(result.current.actionState.message).toBe("Profile created.");
  });

  it("keeps failed form state available for retry", async () => {
    const model = {
      saveCredential: vi.fn().mockRejectedValueOnce(new Error("API unavailable")).mockResolvedValueOnce({ message: "Saved." }),
    };
    const { result } = renderEditor(model);
    act(() => result.current.openCreate("example"));
    act(() => result.current.setFormState({ form: { connector_kind: "example", label: "retry", password: "secret" } }));

    await act(async () => result.current.save({ preventDefault() {} }, "create"));
    expect(result.current.drawer.open).toBe(true);
    expect(result.current.formState.form.password).toBe("secret");
    expect(result.current.actionState.error).toBe("API unavailable");

    await act(async () => result.current.save({ preventDefault() {} }, "create"));
    expect(model.saveCredential).toHaveBeenCalledTimes(2);
    expect(result.current.drawer.open).toBe(false);
  });

  it("loads connector-owned edit state and clears it on cancel", () => {
    const row = { id: 3, connector_kind: "example" };
    const model = { credentialStateFromRow: vi.fn(() => ({ form: { label: "admin", password: "unchanged" } })) };
    const { result } = renderEditor(model);

    act(() => result.current.openEdit(row));
    expect(model.credentialStateFromRow).toHaveBeenCalledWith({ row, targets: [{ id: 4, connector_kind: "example" }] });
    expect(result.current.formState.form.label).toBe("admin");

    act(() => result.current.closeEditor());
    expect(result.current.drawer.open).toBe(false);
    expect(result.current.formState.form.password).toBe("");
  });

  it("accepts an undefined model result as a successful credential save", async () => {
    const model = { saveCredential: vi.fn(async () => undefined) };
    const { result, onRefresh } = renderEditor(model);
    act(() => result.current.openCreate("example"));

    await act(async () => result.current.save({ preventDefault() {} }, "create"));

    expect(result.current.drawer.open).toBe(false);
    expect(result.current.actionState.message).toBe("Credential saved.");
    expect(onRefresh).toHaveBeenCalledOnce();
  });

  it("does not let a retired save close or reset a replacement draft", async () => {
    let resolveSave;
    const pendingSave = new Promise((resolve) => {
      resolveSave = resolve;
    });
    const model = { saveCredential: vi.fn(() => pendingSave) };
    const { result, onRefresh } = renderEditor(model);

    act(() => result.current.openCreate("example"));
    act(() => result.current.setFormState({ form: { connector_kind: "example", label: "old", password: "old-secret" } }));
    let save;
    act(() => {
      save = result.current.save({ preventDefault() {} }, "create");
    });
    act(() => result.current.closeEditor());
    act(() => result.current.openCreate("example"));
    act(() => result.current.setFormState({ form: { connector_kind: "example", label: "replacement", password: "new-secret" } }));

    await act(async () => resolveSave({ message: "Old profile saved." }));

    await expect(save).resolves.toBe(false);
    expect(result.current.drawer.open).toBe(true);
    expect(result.current.formState.form).toEqual({ connector_kind: "example", label: "replacement", password: "new-secret" });
    expect(result.current.actionState.state).toBe("idle");
    expect(onRefresh).not.toHaveBeenCalled();
  });

  it("locks an in-flight draft and submits its snapshot only once", async () => {
    let resolveSave;
    const pendingSave = new Promise((resolve) => {
      resolveSave = resolve;
    });
    const model = { saveCredential: vi.fn(() => pendingSave) };
    const { result } = renderEditor(model);
    act(() => result.current.openCreate("example"));
    act(() => result.current.setFormState({ form: { connector_kind: "example", label: "original", password: "secret" } }));

    let firstSave;
    let secondSave;
    act(() => {
      firstSave = result.current.save({ preventDefault() {} }, "create");
      result.current.setFormState((current) => ({ ...current, form: { ...current.form, label: "broader scope" } }));
      secondSave = result.current.save({ preventDefault() {} }, "create");
    });

    expect(result.current.formState.form.label).toBe("original");
    expect(model.saveCredential).toHaveBeenCalledTimes(1);
    await act(async () => resolveSave({ message: "Saved." }));
    await expect(firstSave).resolves.toBe(true);
    await expect(secondSave).resolves.toBe(false);
    expect(result.current.drawer.open).toBe(false);
  });

  it("unlocks the same draft for correction after a failed save", async () => {
    let rejectSave;
    const pendingSave = new Promise((_, reject) => {
      rejectSave = reject;
    });
    const model = { saveCredential: vi.fn(() => pendingSave) };
    const { result } = renderEditor(model);
    act(() => result.current.openCreate("example"));
    let save;
    act(() => {
      save = result.current.save({ preventDefault() {} }, "create");
    });
    await act(async () => rejectSave(new Error("Network failed")));
    await expect(save).resolves.toBe(false);

    act(() => result.current.setFormState((current) => ({ ...current, form: { ...current.form, label: "corrected" } })));
    expect(result.current.formState.form.label).toBe("corrected");
  });

  it("ignores delayed field updates from a retired editor", () => {
    const { result } = renderEditor({ saveCredential: vi.fn() });
    act(() => result.current.openCreate("example"));
    const staleSetFormState = result.current.setFormState;
    act(() => result.current.closeEditor());
    act(() => result.current.openCreate("example"));
    act(() => result.current.setFormState({ form: { connector_kind: "example", label: "new", password: "new-secret" } }));

    act(() => staleSetFormState((current) => ({ ...current, form: { ...current.form, label: "old" } })));

    expect(result.current.formState.form.label).toBe("new");
  });

  it("keeps a replacement save locked when the retired request settles", async () => {
    const resolvers = [];
    const model = {
      saveCredential: vi.fn(() => new Promise((resolve) => resolvers.push(resolve))),
    };
    const { result } = renderEditor(model);
    act(() => result.current.openCreate("example"));
    let oldSave;
    act(() => {
      oldSave = result.current.save({ preventDefault() {} }, "create");
    });

    act(() => result.current.closeEditor());
    act(() => result.current.openCreate("example"));
    let newSave;
    act(() => {
      newSave = result.current.save({ preventDefault() {} }, "create");
    });
    await act(async () => resolvers[0]({ message: "Old saved." }));
    await expect(oldSave).resolves.toBe(false);

    await act(async () => {
      expect(await result.current.save({ preventDefault() {} }, "create")).toBe(false);
    });
    expect(model.saveCredential).toHaveBeenCalledTimes(2);
    await act(async () => resolvers[1]({ message: "New saved." }));
    await expect(newSave).resolves.toBe(true);
  });

  it("surfaces missing connector behavior without opening an invalid editor", () => {
    const { result } = renderEditor(null);
    const row = { id: 3, connector_kind: "missing" };

    act(() => result.current.openEdit(row));

    expect(result.current.drawer.open).toBe(false);
    expect(result.current.actionState.error).toBe("Connector model not found for missing.");
  });

  it("deletes through connector-owned behavior and supports retry", async () => {
    const row = { id: 3, connector_kind: "example" };
    const model = { deleteCredential: vi.fn().mockRejectedValueOnce(new Error("Delete failed")).mockResolvedValueOnce(undefined) };
    const { result, onRefresh } = renderEditor(model);

    await act(async () => result.current.remove(row));
    expect(result.current.actionState.error).toBe("Delete failed");

    await act(async () => result.current.remove(row));
    expect(model.deleteCredential).toHaveBeenCalledTimes(2);
    expect(onRefresh).toHaveBeenCalledOnce();
    expect(result.current.actionState.message).toBe("Credential deleted.");
  });
});
