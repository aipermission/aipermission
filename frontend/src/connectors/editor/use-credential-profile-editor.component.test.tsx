import { act, renderHook } from "@testing-library/react";
import { StrictMode } from "react";
import { describe, expect, it, vi } from "vitest";
import { useCredentialProfileEditor } from "./use-credential-profile-editor";

import { credentialEditorRenderer, deferred, emptyState, type FormState, type Row } from "../../test/credential-editor-fixtures";
const renderEditor = credentialEditorRenderer(useCredentialProfileEditor);

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
    let resolveSave!: (_value: { message: string }) => void;
    const pendingSave = new Promise<{ message: string }>((resolve) => {
      resolveSave = resolve;
    });
    const model = { saveCredential: vi.fn(() => pendingSave) };
    const { result, onRefresh } = renderEditor(model);

    act(() => result.current.openCreate("example"));
    act(() => result.current.setFormState({ form: { connector_kind: "example", label: "old", password: "old-secret" } }));
    let save: Promise<boolean> | undefined;
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
    let resolveSave!: (_value: { message: string }) => void;
    const pendingSave = new Promise<{ message: string }>((resolve) => {
      resolveSave = resolve;
    });
    const model = { saveCredential: vi.fn(() => pendingSave) };
    const { result } = renderEditor(model);
    act(() => result.current.openCreate("example"));
    act(() => result.current.setFormState({ form: { connector_kind: "example", label: "original", password: "secret" } }));

    let firstSave: Promise<boolean> | undefined;
    let secondSave: Promise<boolean> | undefined;
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
    let rejectSave!: (_error: unknown) => void;
    const pendingSave = new Promise<{ message: string }>((_, reject) => {
      rejectSave = reject;
    });
    const model = { saveCredential: vi.fn(() => pendingSave) };
    const { result } = renderEditor(model);
    act(() => result.current.openCreate("example"));
    let save: Promise<boolean> | undefined;
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

  it("does not submit a retired draft after a replacement editor opens", async () => {
    const model = { saveCredential: vi.fn(async () => ({ message: "Saved." })) };
    const { result, onRefresh } = renderEditor(model);
    act(() => result.current.openCreate("example"));
    act(() => result.current.setFormState({ form: { connector_kind: "example", label: "retired", password: "retired-secret" } }));
    const staleSave = result.current.save;
    act(() => result.current.closeEditor());
    act(() => result.current.openCreate("example"));
    act(() => result.current.setFormState({ form: { connector_kind: "example", label: "replacement", password: "replacement-secret" } }));

    let saved: boolean | undefined;
    await act(async () => {
      saved = await staleSave({ preventDefault() {} }, "create");
    });

    expect(saved).toBe(false);
    expect(model.saveCredential).not.toHaveBeenCalled();
    expect(onRefresh).not.toHaveBeenCalled();
    expect(result.current.drawer.open).toBe(true);
    expect(result.current.formState.form.label).toBe("replacement");
  });

  it("does not delete through a callback retired by a replacement editor", async () => {
    const model = { deleteCredential: vi.fn(async () => undefined) };
    const { result, onRefresh } = renderEditor(model);
    const staleRemove = result.current.remove;
    act(() => result.current.openCreate("example"));
    let removed: boolean | undefined;
    await act(async () => {
      removed = await staleRemove({ id: 3, connector_kind: "example" });
    });
    expect(removed).toBe(false);
    expect(model.deleteCredential).not.toHaveBeenCalled();
    expect(onRefresh).not.toHaveBeenCalled();
    expect(result.current.drawer.open).toBe(true);
  });

  it("keeps a cleared secret empty after a successful save", async () => {
    const model = { saveCredential: vi.fn(async () => ({ message: "Saved." })) };
    const { result } = renderEditor(model);
    act(() => result.current.openCreate("example"));
    act(() => result.current.setFormState({ form: { connector_kind: "example", label: "secret", password: "secret-value" } }));
    const staleSetFormState = result.current.setFormState;

    await act(async () => result.current.save({ preventDefault() {} }, "create"));
    act(() => staleSetFormState((current) => ({ ...current, form: { ...current.form, password: "secret-value" } })));

    expect(result.current.formState.form.password).toBe("");
  });

  it("keeps a replacement save locked when the retired request settles", async () => {
    const resolvers: ((_value: { message: string }) => void)[] = [];
    const model = {
      saveCredential: vi.fn(() => new Promise<{ message: string }>((resolve) => resolvers.push(resolve))),
    };
    const { result } = renderEditor(model);
    act(() => result.current.openCreate("example"));
    let oldSave: Promise<boolean> | undefined;
    act(() => {
      oldSave = result.current.save({ preventDefault() {} }, "create");
    });

    act(() => result.current.closeEditor());
    act(() => result.current.openCreate("example"));
    let newSave: Promise<boolean> | undefined;
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

describe("credential mutation completion ownership", () => {
  it.each(["resolve", "reject"] as const)("retires delete refresh %s when a newer delete is pending", async (settlement) => {
    const refresh = deferred();
    const newer = deferred();
    const model = { deleteCredential: vi.fn().mockResolvedValueOnce(undefined).mockReturnValueOnce(newer.promise) };
    const onRefresh = vi.fn<() => Promise<void>>().mockReturnValueOnce(refresh.promise).mockResolvedValue(undefined);
    const { result } = renderEditor(model, onRefresh);
    let previous!: Promise<boolean>;
    await act(async () => {
      previous = result.current.remove({ id: 1, connector_kind: "example" });
    });
    expect(onRefresh).toHaveBeenCalledOnce();
    act(() => result.current.closeEditor());
    let next!: Promise<boolean>;
    act(() => {
      next = result.current.remove({ id: 2, connector_kind: "example" });
    });
    await act(async () => {
      if (settlement === "resolve") refresh.resolve();
      else refresh.reject(new Error("old refresh failed"));
    });
    await expect(previous).resolves.toBe(false);
    expect(result.current.actionState).toEqual({ state: "deleting", error: null, message: null });
    await act(async () => {
      expect(await result.current.remove({ id: 2, connector_kind: "example" })).toBe(false);
    });
    expect(model.deleteCredential).toHaveBeenCalledTimes(2);
    await act(async () => newer.resolve());
    await expect(next).resolves.toBe(true);
    expect(result.current.actionState.message).toBe("Credential deleted.");
  });

  it.each(["create", "edit"] as const)("does not publish a late delete refresh warning into a new %s editor", async (mode) => {
    const refresh = deferred();
    const model = {
      deleteCredential: vi.fn(async () => {}),
      credentialStateFromRow: () => ({ form: { label: "edited", password: "" } }),
    };
    const { result } = renderEditor(
      model,
      vi.fn(() => refresh.promise),
    );
    let removal!: Promise<boolean>;
    await act(async () => {
      removal = result.current.remove({ id: 1, connector_kind: "example" });
    });
    act(() => {
      if (mode === "create") result.current.openCreate();
      else result.current.openEdit({ id: 2, connector_kind: "example" });
    });
    act(() => result.current.setFormState({ form: { label: "replacement", password: "new-secret" } }));
    await act(async () => refresh.reject(new Error("old refresh failed")));
    await expect(removal).resolves.toBe(false);
    expect(result.current.actionState).toEqual({ state: "idle", error: null, message: null });
    expect(result.current.drawer).toMatchObject({ open: true, mode });
    expect(result.current.formState.form).toEqual({ label: "replacement", password: "new-secret" });
  });

  it("claims a delete synchronously and keeps it claimed throughout refresh", async () => {
    const api = deferred();
    const refresh = deferred();
    const model = { deleteCredential: vi.fn(() => api.promise) };
    const { result } = renderEditor(
      model,
      vi.fn(() => refresh.promise),
    );
    const row = { id: 1, connector_kind: "example" };
    let removal!: Promise<boolean>;
    let duplicate!: Promise<boolean>;
    act(() => {
      removal = result.current.remove(row);
      duplicate = result.current.remove(row);
    });
    expect(model.deleteCredential).toHaveBeenCalledOnce();
    await expect(duplicate).resolves.toBe(false);
    await act(async () => api.resolve());
    await act(async () => {
      expect(await result.current.remove(row)).toBe(false);
    });
    expect(model.deleteCredential).toHaveBeenCalledOnce();
    await act(async () => refresh.resolve());
    await expect(removal).resolves.toBe(true);
  });

  it.each([
    ["api", "resolve"],
    ["api", "reject"],
    ["refresh", "resolve"],
    ["refresh", "reject"],
  ] as const)("ignores delete %s %s after unmount", async (phase, settlement) => {
    const pending = deferred();
    const model = { deleteCredential: vi.fn(() => (phase === "api" ? pending.promise : Promise.resolve())) };
    const onRefresh = vi.fn(() => (phase === "refresh" ? pending.promise : Promise.resolve()));
    const { result, unmount } = renderEditor(model, onRefresh);
    const remove = result.current.remove;
    let removal!: Promise<boolean>;
    await act(async () => {
      removal = remove({ id: 1, connector_kind: "example" });
    });
    unmount();
    await act(async () => {
      if (settlement === "resolve") pending.resolve();
      else pending.reject(new Error("retired failure"));
    });
    await expect(removal).resolves.toBe(false);
    expect(onRefresh).toHaveBeenCalledTimes(phase === "api" ? 0 : 1);
    expect(await remove({ id: 1, connector_kind: "example" })).toBe(false);
    expect(model.deleteCredential).toHaveBeenCalledOnce();
  });

  it("preserves same-owner delete success and a list refresh warning", async () => {
    const { result } = renderEditor(
      { deleteCredential: vi.fn(async () => {}) },
      vi.fn(async () => {
        throw new Error("offline");
      }),
    );
    await act(async () => {
      expect(await result.current.remove({ id: 1, connector_kind: "example" })).toBe(true);
    });
    expect(result.current.actionState).toEqual({
      state: "idle",
      message: "Credential deleted.",
      error: "Saved successfully, but the list refresh failed: offline",
    });
  });

  describe("credential API and lifecycle ownership", () => {
    it.each(["resolve", "reject"] as const)("ignores retired delete API %s without touching a newer pending delete", async (settlement) => {
      const oldAPI = deferred();
      const newAPI = deferred();
      const model = { deleteCredential: vi.fn().mockReturnValueOnce(oldAPI.promise).mockReturnValueOnce(newAPI.promise) };
      const { result, onRefresh } = renderEditor(model);
      let previous!: Promise<boolean>;
      act(() => {
        previous = result.current.remove({ id: 1, connector_kind: "example" });
      });
      act(() => result.current.closeEditor());
      let current!: Promise<boolean>;
      act(() => {
        current = result.current.remove({ id: 2, connector_kind: "example" });
      });
      await act(async () => {
        if (settlement === "resolve") oldAPI.resolve();
        else oldAPI.reject(new Error("old API failed"));
      });
      await expect(previous).resolves.toBe(false);
      expect(onRefresh).not.toHaveBeenCalled();
      expect(result.current.actionState).toEqual({ state: "deleting", error: null, message: null });
      await act(async () => {
        expect(await result.current.remove({ id: 2, connector_kind: "example" })).toBe(false);
      });
      expect(model.deleteCredential).toHaveBeenCalledTimes(2);
      await act(async () => newAPI.resolve());
      await expect(current).resolves.toBe(true);
      expect(onRefresh).toHaveBeenCalledOnce();
    });

    it.each(["resolve", "reject"] as const)("keeps a committed save successful after refresh %s on unmount", async (settlement) => {
      const pending = deferred();
      const model = { saveCredential: vi.fn(async () => ({ message: "Committed." })) };
      const { result, unmount, onRefresh } = renderEditor(
        model,
        vi.fn(() => pending.promise),
      );
      act(() => result.current.openCreate());
      let save!: Promise<boolean>;
      await act(async () => {
        save = result.current.save(null, "create");
      });
      expect(result.current.drawer.open).toBe(false);
      expect(result.current.actionState.message).toBe("Committed.");
      const staleSave = result.current.save;
      unmount();
      await act(async () => {
        if (settlement === "resolve") pending.resolve();
        else pending.reject(new Error("retired refresh failed"));
      });
      await expect(save).resolves.toBe(true);
      expect(await staleSave(null, "create")).toBe(false);
      expect(model.saveCredential).toHaveBeenCalledOnce();
      expect(onRefresh).toHaveBeenCalledOnce();
    });

    it("allows the current callback after StrictMode mount replay", async () => {
      const deleteCredential = vi.fn(async () => {});
      const { result } = renderHook(
        () =>
          useCredentialProfileEditor<FormState, Row>({
            defaultKind: "example",
            targets: [],
            emptyStateForKind: emptyState,
            modelForKind: () => ({ deleteCredential }),
          }),
        { wrapper: StrictMode },
      );
      await act(async () => {
        expect(await result.current.remove({ id: 1, connector_kind: "example" })).toBe(true);
      });
      expect(deleteCredential).toHaveBeenCalledOnce();
    });

    it("keeps a same-owner committed save visible when its refresh fails", async () => {
      const { result } = renderEditor(
        { saveCredential: vi.fn(async () => ({ message: "Committed." })) },
        vi.fn(async () => {
          throw new Error("offline");
        }),
      );
      act(() => result.current.openCreate());
      await act(async () => {
        expect(await result.current.save(null, "create")).toBe(true);
      });
      expect(result.current.actionState).toEqual({
        state: "idle",
        message: "Committed.",
        error: "Saved successfully, but the list refresh failed: offline",
      });
      expect(result.current.drawer.open).toBe(false);
      expect(result.current.formState.form.password).toBe("");
    });
  });
});
