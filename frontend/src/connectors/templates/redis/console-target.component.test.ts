import { expect, it } from "vitest";
import { gatewayTargetFixture } from "../../../test/connector-inventory-fixtures";
import { redisConsoleTarget } from "./console-target";

it("preserves native Redis/Valkey endpoint primitives and excludes unrelated config", () => {
  const target = gatewayTargetFixture({
    connector_kind: "redis",
    ref: "redis:3:11",
    config: { server_family: "valkey", host: "cache.test", port: "6379", database: 0, connection_mode: "ssh", opaque: true },
  });
  expect(redisConsoleTarget(target)).toEqual({
    ref: "redis:3:11",
    connector_kind: "redis",
    config: { server_family: "valkey", host: "cache.test", port: "6379", database: 0, connection_mode: "ssh" },
  });
  expect(redisConsoleTarget({ ...target, config: { database: "1", port: 6379 } }).config).toMatchObject({ database: "1", port: 6379 });
  expect(redisConsoleTarget({ ...target, config: undefined }).config?.database).toBeUndefined();
});

it.each(["server_family", "host", "port", "database", "connection_mode"])("rejects malformed %s instead of coercing it", (field) => {
  const target = gatewayTargetFixture({ config: { [field]: { invalid: true } } });
  expect(() => redisConsoleTarget(target)).toThrow(`Invalid Redis console target ${field}.`);
});
