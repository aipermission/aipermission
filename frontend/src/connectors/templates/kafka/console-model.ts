import { captureConsolePresentation, consolePresentationIdentity } from "../_shared/console-presentation";
import { kafkaConsoleTarget } from "./console-target";
import * as model from "./model";

export const kafkaConsoleModel = captureConsolePresentation({
  decodeTarget: (target) => ({ ...consolePresentationIdentity(target), ...kafkaConsoleTarget(target) }),
  model,
});
