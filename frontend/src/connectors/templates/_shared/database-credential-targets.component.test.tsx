import { expect, it } from "vitest";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { databaseCredentialTargets } from "./database-credential-targets";

it("preserves the complete inventory envelope while decoding database fields", () => {
  const original = inventoryTargetFixture({
    connector_kind: "test-database",
    config: {
      host: "localhost",
      port: "9000",
      database: "test",
      connection_mode: "over_ssh",
      transport_target_ref: "transport:8:9",
      extension: "preserved",
    },
    profiles: [
      inventoryProfileFixture({ connector_kind: "test-database", runtime_id: 91, public: { username: "reader", extension: "preserved" } }),
    ],
  });
  const [decoded] = databaseCredentialTargets([original, inventoryTargetFixture()], "test-database", "Test database");
  expect(decoded).toMatchObject(original);
  expect(decoded).not.toBe(original);
  expect(decoded.profiles?.[0]).not.toBe(original.profiles?.[0]);
  expect(
    databaseCredentialTargets(
      [inventoryTargetFixture({ connector_kind: "test-database", config: undefined, profiles: undefined })],
      "test-database",
      "Test database",
    )[0].profiles,
  ).toEqual([]);
});

it.each(["host", "port", "database", "connection_mode", "transport_target_ref"])("rejects malformed native database %s", (field) => {
  expect(() =>
    databaseCredentialTargets(
      [inventoryTargetFixture({ connector_kind: "test-database", config: { [field]: [] } })],
      "test-database",
      "Test database",
    ),
  ).toThrow(field);
});

it("rejects malformed usernames before rendering native forms", () => {
  expect(() =>
    databaseCredentialTargets(
      [inventoryTargetFixture({ connector_kind: "test-database", profiles: [inventoryProfileFixture({ public: { username: [] } })] })],
      "test-database",
      "Test database",
    ),
  ).toThrow("username");
});
