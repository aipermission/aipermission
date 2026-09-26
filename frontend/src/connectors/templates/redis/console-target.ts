import { optionalConsolePort, optionalConsoleText, optionalConsoleTextOrNumber } from "../_shared/console-target-config";
import type { GatewayTarget } from "../../../lib/gateway-contracts/core-resource-contracts";
import type { RedisBrowserProps } from "./browser-types";

export function redisConsoleTarget(target: GatewayTarget): RedisBrowserProps["target"] {
  const config = target.config || {};
  return {
    ref: target.ref,
    connector_kind: target.connector_kind,
    config: {
      server_family: optionalConsoleText(config.server_family, "Redis", "server_family"),
      host: optionalConsoleText(config.host, "Redis", "host"),
      port: optionalConsolePort(config.port, "Redis"),
      database: optionalConsoleTextOrNumber(config.database, "Redis", "database"),
      connection_mode: optionalConsoleText(config.connection_mode, "Redis", "connection_mode"),
    },
  };
}
