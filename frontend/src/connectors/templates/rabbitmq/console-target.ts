import { optionalConsolePort, optionalConsoleText } from "../_shared/console-target-config";
import type { ConsolePresentationTarget } from "../_shared/console-presentation-types";
import type { RabbitTarget } from "./browser-types";

export function rabbitConsoleTarget(target: ConsolePresentationTarget): RabbitTarget {
  const config = target.config || {};
  return {
    ref: target.ref,
    config: {
      vhost: optionalConsoleText(config.vhost, "RabbitMQ", "vhost"),
      scheme: optionalConsoleText(config.scheme, "RabbitMQ", "scheme"),
      host: optionalConsoleText(config.host, "RabbitMQ", "host"),
      port: optionalConsolePort(config.port, "RabbitMQ"),
      connection_mode: optionalConsoleText(config.connection_mode, "RabbitMQ", "connection_mode"),
    },
  };
}
