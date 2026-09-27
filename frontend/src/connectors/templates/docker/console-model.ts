import { captureConsolePresentation, consolePresentationIdentity } from "../_shared/console-presentation";
import { optionalConsoleText } from "../_shared/console-target-config";
import { captureConsoleRuntimeProjection } from "../_shared/console-runtime-model";
import type { ConsolePresentationTarget } from "../_shared/console-presentation-types";
import * as model from "./model";

export const dockerConsoleModel = captureConsolePresentation({
  kind: "docker",
  decodeTarget: decodePresentation,
  model,
  runtimeTarget: captureConsoleRuntimeProjection(
    (target) => ({ ...target, ...decodePresentation(target) }),
    model.liveConsoleRuntimeTarget,
  ),
});

function decodePresentation(target: ConsolePresentationTarget) {
  return {
    ...consolePresentationIdentity(target),
    config: {
      docker_command: optionalConsoleText(target.config?.docker_command, "Docker", "docker_command"),
      transport_target_ref: optionalConsoleText(target.config?.transport_target_ref, "Docker", "transport_target_ref"),
    },
  };
}
