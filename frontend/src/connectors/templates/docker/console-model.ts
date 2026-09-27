import { captureConsolePresentation, consolePresentationIdentity } from "../_shared/console-presentation";
import { optionalConsoleText } from "../_shared/console-target-config";
import * as model from "./model";

export const dockerConsoleModel = captureConsolePresentation({
  decodeTarget: (target) => ({
    ...consolePresentationIdentity(target),
    config: {
      docker_command: optionalConsoleText(target.config?.docker_command, "Docker", "docker_command"),
      transport_target_ref: optionalConsoleText(target.config?.transport_target_ref, "Docker", "transport_target_ref"),
    },
  }),
  model,
});
