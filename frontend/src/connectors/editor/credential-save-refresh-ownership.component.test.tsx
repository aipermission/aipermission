import { act } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useCredentialProfileEditor } from "./use-credential-profile-editor";
import { credentialEditorRenderer, deferred } from "../../test/credential-editor-fixtures";

const renderEditor = credentialEditorRenderer(useCredentialProfileEditor);

describe("committed credential save refresh ownership", () => {
  it.each([
    ["delete", "resolve"],
    ["delete", "reject"],
    ["save", "resolve"],
    ["save", "reject"],
    ["create", "resolve"],
    ["create", "reject"],
    ["edit", "resolve"],
    ["edit", "reject"],
  ] as const)("preserves newer %s state after a committed save's refresh %s", async (replacement, settlement) => {
    const refresh = deferred();
    const newer = deferred();
    const model = {
      saveCredential: vi.fn().mockResolvedValueOnce({ message: "A committed." }).mockReturnValueOnce(newer.promise),
      deleteCredential: vi.fn(() => newer.promise),
      credentialStateFromRow: () => ({ form: { label: "B", password: "" } }),
    };
    const onRefresh = vi.fn<() => Promise<void>>().mockReturnValueOnce(refresh.promise).mockResolvedValue(undefined);
    const { result } = renderEditor(model, onRefresh);
    act(() => result.current.openCreate());
    let previous!: Promise<boolean>;
    await act(async () => {
      previous = result.current.save(null, "create");
    });
    expect(result.current.drawer.open).toBe(false);
    expect(result.current.actionState.message).toBe("A committed.");
    expect(model.saveCredential).toHaveBeenCalledOnce();
    expect(onRefresh).toHaveBeenCalledOnce();
    let next: Promise<boolean> | undefined;
    if (replacement === "delete")
      act(() => {
        next = result.current.remove({ id: 2, connector_kind: "example" });
      });
    else {
      act(() => {
        if (replacement === "edit") result.current.openEdit({ id: 2, connector_kind: "example" });
        else result.current.openCreate();
      });
      act(() => result.current.setFormState({ form: { label: "B", password: "B-secret" } }));
      if (replacement === "save")
        act(() => {
          next = result.current.save(null, "create");
        });
    }
    const state = result.current.actionState;
    const drawer = result.current.drawer;
    const form = result.current.formState;
    await act(async () => {
      if (settlement === "resolve") refresh.resolve();
      else refresh.reject(new Error("old save refresh failed"));
    });
    // A's API success remains true; only its retired UI publications are suppressed.
    await expect(previous).resolves.toBe(true);
    expect(result.current.actionState).toEqual(state);
    expect(result.current.drawer).toEqual(drawer);
    expect(result.current.formState).toEqual(form);
    if (replacement === "save") {
      await act(async () => {
        expect(await result.current.save(null, "create")).toBe(false);
      });
      expect(model.saveCredential).toHaveBeenCalledTimes(2);
    }
    if (next) {
      await act(async () => newer.resolve());
      await expect(next).resolves.toBe(true);
    }
  });
});
