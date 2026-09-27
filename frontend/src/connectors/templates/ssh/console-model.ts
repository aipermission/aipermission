import { captureConsolePresentation, consolePresentationIdentity } from "../_shared/console-presentation";
import { optionalConsolePort, optionalConsoleText, optionalConsoleTextOrNumber } from "../_shared/console-target-config";
import { captureConsoleRuntimeProjection } from "../_shared/console-runtime-model";
import type { ConsolePresentationTarget } from "../_shared/console-presentation-types";
import * as model from "./model";

export const sshConsoleModel = captureConsolePresentation({
  kind: "ssh",
  decodeTarget: decodePresentation,
  model,
  runtimeTarget: captureConsoleRuntimeProjection((target) => {
    const presentation = decodePresentation(target);
    return {
      ...target,
      ...presentation,
      config: {
        ...presentation.config,
        description: optionalConsoleText(target.config?.description, "SSH", "description"),
        startup_input_after_connect: optionalConsoleText(target.config?.startup_input_after_connect, "SSH", "startup_input_after_connect"),
        force_shell_command: optionalConsoleText(target.config?.force_shell_command, "SSH", "force_shell_command"),
      },
      public: {
        ...presentation.public,
        ssh_key_id: optionalConsoleTextOrNumber(target.public?.ssh_key_id, "SSH", "ssh_key_id"),
      },
    };
  }, model.liveConsoleRuntimeTarget),
  subtitle: ({ target, runtimeTarget }) =>
    model.targetSubtitle({
      target,
      runtimeTarget: runtimeTarget
        ? {
            username: optionalConsoleText(runtimeTarget.username, "SSH", "username"),
            host: optionalConsoleText(runtimeTarget.host, "SSH", "host"),
            port: optionalConsolePort(runtimeTarget.port, "SSH"),
          }
        : runtimeTarget,
    }),
});

function decodePresentation(target: ConsolePresentationTarget) {
  return {
    ...consolePresentationIdentity(target),
    config: {
      host: optionalConsoleText(target.config?.host, "SSH", "host"),
      port: optionalConsolePort(target.config?.port, "SSH"),
    },
    public: { username: optionalConsoleText(target.public?.username, "SSH", "username") },
  };
}
