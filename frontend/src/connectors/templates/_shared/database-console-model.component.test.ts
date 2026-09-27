import { describe, expect, it, vi } from "vitest";
import { clickhouseConsoleModel } from "../clickhouse/console-model";
import { postgresConsoleModel } from "../postgres/console-model";
import { captureDatabaseConsolePresentation } from "./database-console-model";
import type { DatabasePresentationTarget } from "./database-model-types";

it("passes only decoded presentation fields to the native database model", () => {
  const targetDisplayName = vi.fn(({ target }: { target?: DatabasePresentationTarget | null }) => target?.name || "Unnamed");
  const model = captureDatabaseConsolePresentation({
    targetDisplayName,
    targetSubtitle: () => "Endpoint",
    targetProfileLabel: () => "Profile",
    usesLiveConsole: () => false,
    recoverableRunningActions: () => [],
  });
  const target = {
    ref: "database:3:7",
    connector_kind: "database",
    name: "DB",
    target_name: "Display DB",
    profile_label: "Reader",
    config: { host: "db.local", port: 5432, database: "events", connection_mode: "direct", ssl_mode: "auto" },
  };
  expect(model.targetDisplayName({ target })).toBe("DB");
  expect(targetDisplayName.mock.calls[0]).toStrictEqual([
    {
      target: {
        name: "DB",
        target_name: "Display DB",
        profile_label: "Reader",
        config: { host: "db.local", port: 5432, database: "events", connection_mode: "direct" },
      },
    },
  ]);
  expect(target.config.ssl_mode).toBe("auto");
});

describe.each([
  { kind: "postgres", label: "Postgres", model: postgresConsoleModel, fallback: "host:5432/database · direct" },
  { kind: "clickhouse", label: "ClickHouse", model: clickhouseConsoleModel, fallback: "127.0.0.1:9000/default · direct" },
])("$label native console presentation", ({ kind, label, model, fallback }) => {
  it("preserves presentation-only identity and over-SSH endpoint without fabricating an ID", () => {
    const target = {
      ref: `${kind}:3:7`,
      connector_kind: kind,
      target_name: "Main DB",
      profile_label: "Reader",
      config: { host: "db.local", port: "9123", database: "events", connection_mode: "over_ssh" },
    };
    expect(model.targetDisplayName({ target })).toBe("Main DB");
    expect(model.targetSubtitle({ target })).toBe("db.local:9123/events · over ssh");
    expect(model.targetProfileLabel({ target })).toBe("Reader");
    expect(model.usesLiveConsole({ target })).toBe(false);
    expect(model.recoverableRunningActions({ target })).toEqual([]);
  });

  it("retains connector-specific endpoint and common profile defaults", () => {
    expect(model.targetDisplayName({})).toBe(`${label} target`);
    expect(model.targetProfileLabel({ target: null })).toBe("default");
    const target = { ref: `${kind}:3:7`, connector_kind: kind };
    expect(model.targetDisplayName({ target })).toBe(`${label} target`);
    expect(model.targetSubtitle({ target })).toBe(fallback);
  });

  it.each([{ host: 123 }, { port: NaN }, { database: [] }, { connection_mode: false }])(
    "rejects malformed presentation config %j",
    (config) => {
      expect(() => model.targetSubtitle({ target: { ref: `${kind}:3:7`, connector_kind: kind, config } })).toThrow(
        "Invalid SQL console target",
      );
    },
  );
});
