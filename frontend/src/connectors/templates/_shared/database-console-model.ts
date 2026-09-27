import { captureConsolePresentation, consolePresentationIdentity } from "./console-presentation";
import { optionalConsolePort, optionalConsoleText } from "./console-target-config";
import type { NativeConsolePresentation } from "./console-presentation-types";
import type { DatabasePresentationTarget } from "./database-model-types";

export function captureDatabaseConsolePresentation(model: NativeConsolePresentation<DatabasePresentationTarget>["model"]) {
  return captureConsolePresentation({
    model,
    decodeTarget(target): DatabasePresentationTarget {
      const config = target.config || {};
      return {
        ...consolePresentationIdentity(target),
        config: {
          host: optionalConsoleText(config.host, "SQL", "host"),
          port: optionalConsolePort(config.port, "SQL"),
          database: optionalConsoleText(config.database, "SQL", "database"),
          connection_mode: optionalConsoleText(config.connection_mode, "SQL", "connection_mode"),
        },
      };
    },
  });
}
