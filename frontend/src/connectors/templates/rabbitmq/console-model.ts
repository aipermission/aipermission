import { captureConsolePresentation, consolePresentationIdentity } from "../_shared/console-presentation";
import { rabbitConsoleTarget } from "./console-target";
import * as model from "./model";

export const rabbitConsoleModel = captureConsolePresentation({
  kind: "rabbitmq",
  decodeTarget: (target) => ({ ...consolePresentationIdentity(target), ...rabbitConsoleTarget(target) }),
  model,
});
