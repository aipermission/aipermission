import { act, renderHook, waitFor } from "@testing-library/react";
import type { FormEvent } from "react";
import { useLayoutEffect } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPut } from "../../lib/api";
import { capabilitySnapshot, deferred, scopeSnapshot } from "../../test/tokens/vault-permission-support";
import { useVaultPermissionEditor } from "./use-vault-permission-editor";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPut: vi.fn() }));

function submitEvent() {
  return { preventDefault: vi.fn() } as unknown as FormEvent<HTMLFormElement>;
}

beforeEach(() => {
  vi.mocked(apiGet)
    .mockReset()
    .mockImplementation(async (path) => (path.endsWith("/project-scopes") ? scopeSnapshot() : capabilitySnapshot()));
  vi.mocked(apiPut)
    .mockReset()
    .mockImplementation(async (path) =>
      path.endsWith("/project-scopes") ? scopeSnapshot("scope-2", false) : capabilitySnapshot("capability-2"),
    );
});

describe("Vault editor retained public handlers", () => {
  it.each(["close", "switch", "ABA", "unmount"])(
    "rejects retained save and scope handlers after %s before transport admission",
    async (transition) => {
      const onSaved = vi.fn();
      const hook = renderHook(({ tokenID }) => useVaultPermissionEditor(tokenID, onSaved), {
        initialProps: { tokenID: 7 as number | undefined },
      });
      await waitFor(() => expect(hook.result.current.load.state).toBe("ready"));
      const oldSave = hook.result.current.saveCapabilities;
      const oldToggle = hook.result.current.toggleProjectScope;
      if (transition === "unmount") hook.unmount();
      else {
        hook.rerender({ tokenID: transition === "close" ? undefined : 8 });
        if (transition === "ABA") hook.rerender({ tokenID: 7 });
        if (transition !== "close") await waitFor(() => expect(hook.result.current.load.state).toBe("ready"));
      }
      await act(async () => {
        await oldSave(submitEvent());
        await oldToggle(3, false);
      });
      expect(apiPut).not.toHaveBeenCalled();
      expect(onSaved).not.toHaveBeenCalled();
      if (transition !== "unmount") {
        if (transition === "close") {
          hook.rerender({ tokenID: 7 });
          await waitFor(() => expect(hook.result.current.load.state).toBe("ready"));
        }
        expect(hook.result.current.load.capabilityRevision).toBe("capability-1");
        expect(hook.result.current.load.scopeRevision).toBe("scope-1");
        expect(hook.result.current.scopeDraft[3]).toBe(true);
        const fresh = deferred();
        vi.mocked(apiPut).mockReturnValueOnce(fresh.promise);
        let saving!: Promise<void>;
        act(() => {
          saving = hook.result.current.saveCapabilities(submitEvent());
        });
        const signal = vi.mocked(apiPut).mock.calls[0][2]?.signal;
        await act(async () => {
          await oldSave(submitEvent());
          await oldToggle(3, false);
        });
        expect(apiPut).toHaveBeenCalledOnce();
        expect(signal?.aborted).toBe(false);
        await act(async () => {
          fresh.resolve(capabilitySnapshot("fresh-revision"));
          await saving;
        });
        expect(hook.result.current.load.capabilityRevision).toBe("fresh-revision");
        expect(onSaved).toHaveBeenCalledOnce();
      }
    },
  );

  it("retires a saved handler when its draft snapshot is no longer committed", async () => {
    const hook = renderHook(() => useVaultPermissionEditor(7));
    await waitFor(() => expect(hook.result.current.load.state).toBe("ready"));
    const oldSave = hook.result.current.saveCapabilities;
    act(() => hook.result.current.setCapabilityRule(3, "vault.inject", "always_run"));
    await act(async () => oldSave(submitEvent()));
    expect(apiPut).not.toHaveBeenCalled();
    expect(hook.result.current.capabilityDraft["3:vault.inject"].execution_rule).toBe("always_run");
    vi.mocked(apiPut).mockResolvedValueOnce(capabilitySnapshot("capability-2", "always_run"));
    await act(async () => hook.result.current.saveCapabilities(submitEvent()));
    expect(vi.mocked(apiPut).mock.calls[0][1]).toMatchObject({
      expected_revision: "capability-1",
      capabilities: [{ execution_rule: "always_run" }],
    });
  });

  it("pairs a pre-commit save snapshot with its own draft version, not a newer edit version", async () => {
    const pending = deferred();
    const hook = renderHook(() => useVaultPermissionEditor(7));
    await waitFor(() => expect(hook.result.current.load.state).toBe("ready"));
    const oldSave = hook.result.current.saveCapabilities;
    vi.mocked(apiPut).mockReturnValueOnce(pending.promise);
    let saving!: Promise<void>;
    act(() => {
      hook.result.current.setCapabilityRule(3, "vault.inject", "always_run");
      saving = oldSave(submitEvent());
    });
    expect(vi.mocked(apiPut).mock.calls[0][1]).toEqual({ capabilities: [], expected_revision: "capability-1" });
    await act(async () => {
      pending.resolve(capabilitySnapshot("capability-2"));
      await saving;
    });
    expect(hook.result.current.capabilityDraft["3:vault.inject"].execution_rule).toBe("always_run");
    expect(hook.result.current.save.state).toBe("unsaved");
    vi.mocked(apiPut).mockResolvedValueOnce(capabilitySnapshot("capability-3", "always_run"));
    await act(async () => hook.result.current.saveCapabilities(submitEvent()));
    expect(vi.mocked(apiPut).mock.calls[1][1]).toMatchObject({
      expected_revision: "capability-2",
      capabilities: [{ execution_rule: "always_run" }],
    });
  });

  it("rejects handlers retaining a prior revision after a save, but permits unchanged same-identity handlers", async () => {
    const hook = renderHook(({ tokenID }) => useVaultPermissionEditor(tokenID), { initialProps: { tokenID: 7 } });
    await waitFor(() => expect(hook.result.current.load.state).toBe("ready"));
    const save = hook.result.current.saveCapabilities;
    const toggle = hook.result.current.toggleProjectScope;
    hook.rerender({ tokenID: 7 });
    expect(hook.result.current.saveCapabilities).toBe(save);
    expect(hook.result.current.toggleProjectScope).toBe(toggle);
    await act(async () => save(submitEvent()));
    await act(async () => save(submitEvent()));
    expect(apiPut).toHaveBeenCalledOnce();
    await act(async () => hook.result.current.saveCapabilities(submitEvent()));
    expect(vi.mocked(apiPut).mock.calls[1][1]).toMatchObject({ expected_revision: "capability-2" });
    const oldToggle = hook.result.current.toggleProjectScope;
    await act(async () => oldToggle(3, false));
    await act(async () => oldToggle(3, true));
    expect(apiPut).toHaveBeenCalledTimes(3);
    await act(async () => hook.result.current.toggleProjectScope(3, true));
    expect(vi.mocked(apiPut).mock.calls[3][1]).toEqual({ enabled_project_ids: [3], expected_revision: "scope-2" });
  });
});

describe("Vault editor commit-boundary admission", () => {
  it("rejects retired and not-yet-loaded handlers during the token switch layout", async () => {
    const scopes = deferred();
    const capabilities = deferred();
    vi.mocked(apiGet).mockImplementation(async (path) => {
      if (path === "/api/tokens/8/project-scopes") return scopes.promise;
      if (path === "/api/tokens/8/project-capabilities") return capabilities.promise;
      return path.endsWith("/project-scopes") ? scopeSnapshot() : capabilitySnapshot();
    });
    let retained: ReturnType<typeof useVaultPermissionEditor> | undefined;
    const hook = renderHook(
      ({ tokenID }) => {
        const editor = useVaultPermissionEditor(tokenID);
        const {
          load: { tokenID: loadedTokenID },
          saveCapabilities,
          toggleProjectScope,
        } = editor;
        useLayoutEffect(() => {
          if (tokenID !== 8 || loadedTokenID === 8) return;
          void retained?.saveCapabilities(submitEvent());
          void retained?.toggleProjectScope(3, false);
          void saveCapabilities(submitEvent());
          void toggleProjectScope(3, false);
        }, [tokenID, loadedTokenID, saveCapabilities, toggleProjectScope]);
        return editor;
      },
      { initialProps: { tokenID: 7 } },
    );
    await waitFor(() => expect(hook.result.current.load.state).toBe("ready"));
    retained = hook.result.current;
    hook.rerender({ tokenID: 8 });
    expect(apiPut).not.toHaveBeenCalled();
    await act(async () => {
      scopes.resolve(scopeSnapshot("scope-8"));
      capabilities.resolve(capabilitySnapshot("capability-8"));
    });
    expect(hook.result.current.load.state).toBe("ready");
    await act(async () => hook.result.current.saveCapabilities(submitEvent()));
    expect(apiPut).toHaveBeenCalledWith(
      "/api/tokens/8/project-capabilities",
      { capabilities: [], expected_revision: "capability-8" },
      { signal: expect.any(AbortSignal) },
    );
  });

  it.each(["capabilities", "scope"])("retires the old %s snapshot as soon as its response advances the revision", async (channel) => {
    let retainedSave!: () => Promise<void>;
    const onSaved = vi
      .fn()
      .mockImplementationOnce(() => retainedSave())
      .mockResolvedValue(undefined);
    const hook = renderHook(() => useVaultPermissionEditor(7, onSaved));
    await waitFor(() => expect(hook.result.current.load.state).toBe("ready"));
    const editor = hook.result.current;
    retainedSave = channel === "capabilities" ? () => editor.saveCapabilities(submitEvent()) : () => editor.toggleProjectScope(3, false);
    await act(async () => retainedSave());
    expect(apiPut).toHaveBeenCalledOnce();
    await act(async () => {
      if (channel === "capabilities") await hook.result.current.saveCapabilities(submitEvent());
      else await hook.result.current.toggleProjectScope(3, true);
    });
    expect(apiPut).toHaveBeenCalledTimes(2);
    expect(vi.mocked(apiPut).mock.calls[1][1]).toMatchObject({
      expected_revision: channel === "capabilities" ? "capability-2" : "scope-2",
    });
  });
});

describe("Vault editor canonical error feedback", () => {
  it.each(["load", "capabilities", "scope", "refresh"])("uses canonical errors for %s", async (channel) => {
    const errors = [new Error(""), "  Permission unavailable  ", "   ", null];
    for (const error of errors) {
      const expected = error === errors[1] ? "Permission unavailable" : "Vault permission request failed.";
      const onSaved = vi.fn().mockRejectedValue(error);
      if (channel === "load") vi.mocked(apiGet).mockRejectedValue(error);
      const hook = renderHook(() => useVaultPermissionEditor(7, channel === "refresh" ? onSaved : undefined));
      await waitFor(() => expect(hook.result.current.load.state).toBe(channel === "load" ? "error" : "ready"));
      if (channel === "load") expect(hook.result.current.load.error).toBe(expected);
      else if (channel === "scope") {
        vi.mocked(apiPut).mockRejectedValueOnce(error);
        await act(async () => hook.result.current.toggleProjectScope(3, false));
        expect(hook.result.current.scopeSave.error).toBe(expected);
      } else {
        if (channel === "capabilities") vi.mocked(apiPut).mockRejectedValueOnce(error);
        await act(async () => hook.result.current.saveCapabilities(submitEvent()));
        expect(hook.result.current.save.error).toBe(
          channel === "refresh" ? `Vault capabilities saved, but refreshing token data failed: ${expected}` : expected,
        );
      }
      hook.unmount();
    }
  });
});
