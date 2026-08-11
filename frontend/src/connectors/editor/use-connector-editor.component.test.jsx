import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useConnectorEditor } from "./use-connector-editor";

const baseForm = (kind) => ({ connector_kind: kind, name: "", project_id: "" });

function renderEditor(model) {
  const onRefresh = vi.fn(async () => {});
  const onOperation = vi.fn(() => true);
  const modelForKind = vi.fn(() => model);
  const hook = renderHook(() =>
    useConnectorEditor({
      defaultKind: "example",
      firstCredentialID: "4",
      defaultProjectID: "2",
      emptyFormForKind: baseForm,
      modelForKind,
      onRefresh,
      onOperation,
    }),
  );
  return { ...hook, onRefresh, onOperation, modelForKind };
}

describe("useConnectorEditor", () => {
  it("tracks dirty state, saves through the connector model, and clears sensitive form state", async () => {
    const model = { save: vi.fn(async () => {}), syncForm: ({ form }) => form };
    const { result, onRefresh } = renderEditor(model);

    act(() => result.current.openCreate("example"));
    act(() => result.current.updateField("name", "Production"));
    expect(result.current.dirty).toBe(true);

    await act(async () => result.current.save({ preventDefault: vi.fn() }));

    expect(model.save).toHaveBeenCalledWith({
      mode: "create",
      form: { connector_kind: "example", name: "Production", project_id: "2" },
      target: null,
    });
    expect(onRefresh).toHaveBeenCalledOnce();
    expect(result.current.drawer.open).toBe(false);
    expect(result.current.form.name).toBe("");
    expect(result.current.dirty).toBe(false);
    expect(result.current.actionState.message).toBe("Connector created.");
  });

  it("keeps the editor open after an API failure and supports retry", async () => {
    const model = {
      save: vi.fn().mockRejectedValueOnce(new Error("API unavailable")).mockResolvedValueOnce(undefined),
      syncForm: ({ form }) => form,
    };
    const { result } = renderEditor(model);
    act(() => result.current.openCreate("example"));
    act(() => result.current.updateField("name", "Retry target"));

    await act(async () => result.current.save({ preventDefault() {} }));
    expect(result.current.drawer.open).toBe(true);
    expect(result.current.actionState).toEqual({ state: "error", error: "API unavailable", message: "" });
    expect(result.current.dirty).toBe(true);

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
    expect(result.current.dirty).toBe(false);
  });

  it("hands connector-owned recovery operations back to the route", async () => {
    const recovery = { open: true, connector_kind: "example", type: "trust" };
    const model = {
      save: vi.fn(async () => Promise.reject(new Error("Trust required"))),
      syncForm: ({ form }) => form,
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
    const model = { deleteTarget: vi.fn(async () => {}), syncForm: ({ form }) => form };
    const { result, onRefresh } = renderEditor(model);

    act(() => result.current.requestDelete(target));
    expect(result.current.deleteDialog).toEqual({ open: true, target });

    await act(async () => result.current.remove(false));

    expect(model.deleteTarget).toHaveBeenCalledWith({ target, removeKey: false });
    expect(result.current.deleteDialog).toEqual({ open: false, target: null });
    expect(result.current.actionState.message).toBe("Connector deleted.");
    expect(onRefresh).toHaveBeenCalledOnce();
  });
});
