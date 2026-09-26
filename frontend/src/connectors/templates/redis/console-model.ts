import { captureConsolePresentation, consolePresentationIdentity } from "../_shared/console-presentation";
import { redisConsoleTarget } from "./console-target";
import * as model from "./model";

export const redisConsoleModel = captureConsolePresentation({
  decodeTarget: (target) => ({ ...consolePresentationIdentity(target), ...redisConsoleTarget(target) }),
  model,
});
