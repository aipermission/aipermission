import { captureConsolePresentation, consolePresentationIdentity } from "../_shared/console-presentation";
import { s3ConsoleTarget } from "./console-target";
import * as model from "./model";

export const s3ConsoleModel = captureConsolePresentation({
  decodeTarget: (target) => ({ ...consolePresentationIdentity(target), ...s3ConsoleTarget(target) }),
  model,
});
