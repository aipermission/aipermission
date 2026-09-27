import { captureConsolePresentation, consolePresentationIdentity } from "../_shared/console-presentation";
import { optionalConsoleText } from "../_shared/console-target-config";
import * as model from "./model";

export const kubernetesConsoleModel = captureConsolePresentation({
  decodeTarget: (target) => ({
    ...consolePresentationIdentity(target),
    config: {
      kubectl_command: optionalConsoleText(target.config?.kubectl_command, "Kubernetes", "kubectl_command"),
      transport_target_ref: optionalConsoleText(target.config?.transport_target_ref, "Kubernetes", "transport_target_ref"),
      context: optionalConsoleText(target.config?.context, "Kubernetes", "context"),
      default_namespace: optionalConsoleText(target.config?.default_namespace, "Kubernetes", "default_namespace"),
    },
  }),
  model,
});
