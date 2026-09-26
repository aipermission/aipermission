import { optionalConsolePort, optionalConsoleText } from "../_shared/console-target-config";
import type { GatewayTarget } from "../../../lib/gateway-contracts/core-resource-contracts";
import type { RabbitTarget } from "./browser-types";

export function rabbitConsoleTarget(target: GatewayTarget): RabbitTarget {
  const config = target.config || {};
  return {
    ref: target.ref,
    config: {
      vhost: optionalConsoleText(config.vhost, "RabbitMQ", "vhost"),
      scheme: optionalConsoleText(config.scheme, "RabbitMQ", "scheme"),
      host: optionalConsoleText(config.host, "RabbitMQ", "host"),
      port: optionalConsolePort(config.port, "RabbitMQ"),
    },
  };
}
