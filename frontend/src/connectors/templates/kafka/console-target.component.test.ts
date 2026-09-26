import { expect, it } from "vitest";
import { gatewayTargetFixture } from "../../../test/connector-inventory-fixtures";
import { kafkaConsoleTarget } from "./console-target";

it("preserves Kafka/Redpanda browser metadata without exposing connection-only settings", () => {
  const target = gatewayTargetFixture({
    connector_kind: "kafka",
    ref: "kafka:3:11",
    config: { server_family: "redpanda", bootstrap_brokers: "broker-a.test:9092,broker-b.test:9092", tls_ca_pem: "not a browser field" },
  });
  expect(kafkaConsoleTarget(target)).toEqual({
    ref: "kafka:3:11",
    config: { server_family: "redpanda", bootstrap_brokers: "broker-a.test:9092,broker-b.test:9092" },
  });
  expect(kafkaConsoleTarget({ ...target, config: undefined }).config?.bootstrap_brokers).toBeUndefined();
});

it.each(["server_family", "bootstrap_brokers"])("rejects malformed %s before opening the Kafka browser", (field) => {
  expect(() => kafkaConsoleTarget(gatewayTargetFixture({ config: { [field]: [] } }))).toThrow(`Invalid Kafka console target ${field}.`);
});
