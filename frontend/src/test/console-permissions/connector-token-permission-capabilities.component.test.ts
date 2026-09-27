import { expect, it } from "vitest";
import { targetSupportsMessages } from "../../components/console/connector-token-permission-model";

it.each(["ssh", "docker", "kubernetes"])("uses native %s live-console capability with a sparse permission target", (connector_kind) => {
  expect(targetSupportsMessages({ connector_kind, runtime_id: 9 })).toBe(true);
  expect(targetSupportsMessages({ connector_kind, runtime_id: 9, ref: `${connector_kind}:3:7` })).toBe(true);
});

it.each(["postgres", "clickhouse", "redis", "rabbitmq", "kafka", "mail", "s3", "missing", "constructor", "toString", "__proto__"])(
  "does not manufacture message support for %s permission rows",
  (connector_kind) => {
    expect(targetSupportsMessages({ connector_kind, runtime_id: 9 })).toBe(false);
  },
);

it("does not advertise messages when runtime or connector identity is absent", () => {
  expect(targetSupportsMessages(null)).toBe(false);
  expect(targetSupportsMessages(undefined)).toBe(false);
  expect(targetSupportsMessages({ connector_kind: "ssh" })).toBe(false);
  expect(targetSupportsMessages({ connector_kind: "ssh", runtime_id: 0 })).toBe(false);
  expect(targetSupportsMessages({ runtime_id: 9 })).toBe(false);
  expect(targetSupportsMessages({ ref: "unknown:3:7", runtime_id: 9 })).toBe(false);
});
