import { captureConsolePresentation, consolePresentationIdentity } from "../_shared/console-presentation";
import { mailConsoleTarget } from "./console-target";
import * as model from "./model";

export const mailConsoleModel = captureConsolePresentation({
  kind: "mail",
  decodeTarget: (target) => ({ ...consolePresentationIdentity(target), ...mailConsoleTarget(target) }),
  model,
});
