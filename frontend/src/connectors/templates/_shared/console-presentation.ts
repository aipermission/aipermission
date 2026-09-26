import type { ConsolePresentationModel, ConsolePresentationTarget, NativeConsolePresentation } from "./console-presentation-types";

export function captureConsolePresentation<Target>(definition: NativeConsolePresentation<Target>): Readonly<ConsolePresentationModel> {
  function decode(target: ConsolePresentationTarget | null | undefined) {
    return target ? definition.decodeTarget(target) : target;
  }
  return Object.freeze({
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
  });
}

export function consolePresentationIdentity(target: ConsolePresentationTarget) {
  return { name: target.name, target_name: target.target_name, profile_label: target.profile_label };
}
