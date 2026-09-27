import { describe, expect, it, vi } from "vitest";
import { captureConsolePresentation, consolePresentationIdentity, isConsolePresentationModel } from "./console-presentation";
import type { ConsolePresentationTarget } from "./console-presentation-types";

const target: ConsolePresentationTarget = {
  ref: "example:3:7",
  connector_kind: "example",
  name: "Target",
  target_name: "Display target",
  profile_label: "Reader",
  config: { label: "native" },
};

function fixture(customSubtitle = false) {
  const decodeTarget = vi.fn((value: ConsolePresentationTarget) => ({ label: String(value.config?.label) }));
  const model = {
    targetDisplayName: vi.fn(({ target }: { target?: { label: string } | null }) => target?.label || "Missing"),
    targetSubtitle: vi.fn(({ target }: { target: { label: string } }) => target.label),
    targetProfileLabel: vi.fn(({ target }: { target?: { label: string } | null }) => target?.label || "Default"),
    usesLiveConsole: vi.fn(({ target }: { target?: { label: string } | null }) => Boolean(target)),
    recoverableRunningActions: vi.fn(({ target }: { target?: { label: string } | null }) => (target ? ["read"] : [])),
  };
  const subtitle = vi.fn(
    ({ target, runtimeTarget }: { target: { label: string }; runtimeTarget?: { name: string } | null }) =>
      `${target.label}:${runtimeTarget?.name || "offline"}`,
  );
  return {
    model,
    decodeTarget,
    subtitle,
    captured: captureConsolePresentation({ kind: "example", decodeTarget, model, ...(customSubtitle ? { subtitle } : {}) }),
  };
}

describe("captured console presentation", () => {
  it("accepts only captured model identities, not structural lookalikes", () => {
    const { captured, model, decodeTarget } = fixture();
    expect(isConsolePresentationModel(captured)).toBe(true);
    for (const value of [null, undefined, false, [], () => {}, { ...captured }]) expect(isConsolePresentationModel(value)).toBe(false);
    expect(() => captureConsolePresentation({ kind: " ", model, decodeTarget })).toThrow("requires a connector kind");
  });
  it("delegates each presentation operation through the native decoder", () => {
    const { captured, model, decodeTarget } = fixture();
    expect(captured.targetDisplayName({ target })).toBe("native");
    expect(captured.targetSubtitle({ target })).toBe("native");
    expect(captured.targetProfileLabel({ target })).toBe("native");
    expect(captured.usesLiveConsole({ target })).toBe(true);
    expect(captured.recoverableRunningActions({ target })).toEqual(["read"]);
    expect(decodeTarget).toHaveBeenCalledTimes(5);
    for (const operation of Object.values(model)) expect(operation).toHaveBeenCalledWith({ target: { label: "native" } });
    expect(Object.isFrozen(captured)).toBe(true);
  });

  it.each([undefined, null])("preserves absent targets without calling the native decoder: %s", (target) => {
    const { captured, model, decodeTarget } = fixture();
    expect(captured.targetDisplayName({ target })).toBe("Missing");
    expect(captured.targetProfileLabel({ target })).toBe("Default");
    expect(captured.usesLiveConsole({ target })).toBe(false);
    expect(captured.recoverableRunningActions({ target })).toEqual([]);
    expect(model.targetDisplayName).toHaveBeenCalledWith({ target });
    expect(decodeTarget).not.toHaveBeenCalled();
  });

  it("lets a native subtitle include live runtime metadata without changing other methods", () => {
    const { captured, subtitle, model } = fixture(true);
    expect(captured.targetSubtitle({ target, runtimeTarget: { id: 9, name: "Live" } })).toBe("native:Live");
    expect(captured.targetSubtitle({ target, runtimeTarget: null })).toBe("native:offline");
    expect(subtitle).toHaveBeenCalledTimes(2);
    expect(model.targetSubtitle).not.toHaveBeenCalled();
  });

  it("does not hide native decoding failures", () => {
    const captured = captureConsolePresentation({
      ...fixture(),
      kind: "example",
      decodeTarget() {
        throw new Error("Invalid native config");
      },
    });
    expect(() => captured.targetSubtitle({ target })).toThrow("Invalid native config");
  });

  it("exposes only explicitly registered read services and forwards request options intact", async () => {
    const { model, decodeTarget } = fixture();
    const loadCredentialResources = vi.fn().mockResolvedValue([{ id: 3, name: "Credential" }]);
    const runtimeTarget = vi.fn();
    const captured = captureConsolePresentation({
      kind: "example",
      decodeTarget,
      model: { ...model, loadCredentialResources },
      runtimeTarget,
    });
    const options = { signal: new AbortController().signal, timeoutMs: 4000 };
    await expect(captured.loadCredentialResources?.(options)).resolves.toEqual([{ id: 3, name: "Credential" }]);
    expect(loadCredentialResources).toHaveBeenCalledWith(options);
    expect(loadCredentialResources.mock.calls[0][0]).toBe(options);
    expect(captured.liveConsoleRuntimeTarget).toBe(runtimeTarget);
    expect(fixture().captured).not.toHaveProperty("loadCredentialResources");
    expect(fixture().captured).not.toHaveProperty("liveConsoleRuntimeTarget");
  });

  it("copies presentation identity without manufacturing persistence identities", () => {
    expect(consolePresentationIdentity(target)).toEqual({ name: "Target", target_name: "Display target", profile_label: "Reader" });
    expect(consolePresentationIdentity({ ref: "example:0:0", connector_kind: "example" })).toEqual({
      name: undefined,
      target_name: undefined,
      profile_label: undefined,
    });
  });
});
