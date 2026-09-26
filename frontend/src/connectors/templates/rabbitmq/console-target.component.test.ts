import { expect, it } from "vitest";
import { gatewayTargetFixture } from "../../../test/connector-inventory-fixtures";
import { rabbitConsoleTarget } from "./console-target";

it("preserves the RabbitMQ management endpoint without importing unrelated target data", () => {
  const target = gatewayTargetFixture({
    connector_kind: "rabbitmq",
    ref: "rabbitmq:3:11",
    config: { vhost: "/app", scheme: "https", host: "queue.test", port: "15671", opaque: true },
  });
  expect(rabbitConsoleTarget(target)).toEqual({
    ref: "rabbitmq:3:11",
    config: { vhost: "/app", scheme: "https", host: "queue.test", port: "15671" },
  });
  expect(rabbitConsoleTarget({ ...target, config: undefined }).config?.vhost).toBeUndefined();
  expect(rabbitConsoleTarget({ ...target, config: { port: 15672 } }).config?.port).toBe(15672);
});

it.each(["vhost", "scheme", "host", "port"])("rejects malformed %s before opening the queue browser", (field) => {
  const target = gatewayTargetFixture({ config: { [field]: [] } });
  expect(() => rabbitConsoleTarget(target)).toThrow(`Invalid RabbitMQ console target ${field}.`);
});
