import { optionalConsoleText } from "../_shared/console-target-config";
import type { GatewayTarget } from "../../../lib/gateway-contracts/core-resource-contracts";
import type { KafkaBrowserProps } from "./console-types";

export function kafkaConsoleTarget(target: GatewayTarget): KafkaBrowserProps["target"] {
  const config = target.config || {};
  return {
    ref: target.ref,
    config: {
      server_family: optionalConsoleText(config.server_family, "Kafka", "server_family"),
      bootstrap_brokers: optionalConsoleText(config.bootstrap_brokers, "Kafka", "bootstrap_brokers"),
    },
  };
}
