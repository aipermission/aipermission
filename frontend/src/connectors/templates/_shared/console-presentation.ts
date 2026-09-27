import type { ConsolePresentationModel, ConsolePresentationTarget, NativeConsolePresentation } from "./console-presentation-types";

const capturedModels = new WeakSet<object>();

export function captureConsolePresentation<Target>(definition: NativeConsolePresentation<Target>): Readonly<ConsolePresentationModel> {
  if (!definition.kind.trim()) throw new Error("Console presentation requires a connector kind.");
  function decode(target: ConsolePresentationTarget | null | undefined) {
    return target ? definition.decodeTarget(target) : target;
  }
  const captured: ConsolePresentationModel = {
    kind: definition.kind,
    targetDisplayName: ({ target }) => definition.model.targetDisplayName({ target: decode(target) }),
    targetSubtitle: ({ target, runtimeTarget }) => {
      const native = definition.decodeTarget(target);
      return definition.subtitle
        ? definition.subtitle({ target: native, runtimeTarget })
        : definition.model.targetSubtitle({ target: native });
    },
    targetProfileLabel: ({ target }) => definition.model.targetProfileLabel({ target: decode(target) }),
    usesLiveConsole: ({ target }) => definition.model.usesLiveConsole({ target: decode(target) }),
    recoverableRunningActions: ({ target }) => definition.model.recoverableRunningActions({ target: decode(target) }),
    ...(definition.model.loadCredentialResources ? { loadCredentialResources: definition.model.loadCredentialResources } : {}),
    ...(definition.runtimeTarget ? { liveConsoleRuntimeTarget: definition.runtimeTarget } : {}),
  };
  capturedModels.add(captured);
  return Object.freeze(captured);
}

export function isConsolePresentationModel(value: unknown): value is Readonly<ConsolePresentationModel> {
  return value !== null && typeof value === "object" && capturedModels.has(value);
}

export function consolePresentationIdentity(target: ConsolePresentationTarget) {
  return { name: target.name, target_name: target.target_name, profile_label: target.profile_label };
}
