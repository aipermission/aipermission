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
    expect(result.current.dirty).toBe(false);
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
    expect(result.current.dirty).toBe(false);
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
