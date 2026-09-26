import { describe, expect, it } from "vitest";
import { redisConsoleModel } from "./console-model";

describe("Redis console presentation", () => {
  it("preserves product, profile, endpoint and transport labels without a database ID", () => {
    const target = {
      ref: "redis:3:7",
      connector_kind: "redis",
      target_name: "Cache",
      profile_label: "Reader",
      config: { server_family: "valkey", host: "cache.local", port: "6380", database: "2", connection_mode: "over_ssh" },
    };
    expect(redisConsoleModel.targetDisplayName({ target })).toBe("Cache");
    expect(redisConsoleModel.targetSubtitle({ target })).toBe("Valkey · cache.local:6380/2 · over ssh");
    expect(redisConsoleModel.targetProfileLabel({ target })).toBe("Reader");
    expect(redisConsoleModel.usesLiveConsole({ target })).toBe(false);
    expect(redisConsoleModel.recoverableRunningActions({ target })).toEqual([]);
  });

  it("retains missing-target and empty-config defaults", () => {
    expect(redisConsoleModel.targetDisplayName({})).toBe("Redis / Valkey target");
    expect(redisConsoleModel.targetProfileLabel({ target: null })).toBe("default");
    const target = { ref: "redis:3:7", connector_kind: "redis" };
    expect(redisConsoleModel.targetDisplayName({ target })).toBe("Redis target");
    expect(redisConsoleModel.targetSubtitle({ target })).toBe("Redis · 127.0.0.1:6379/0 · direct");
  });

  it.each([{ host: 123 }, { port: {} }, { database: NaN }, { server_family: false }, { connection_mode: [] }])(
    "rejects malformed native presentation config %j",
    (config) => {
      expect(() => redisConsoleModel.targetSubtitle({ target: { ref: "redis:3:7", connector_kind: "redis", config } })).toThrow(
        "Invalid Redis console target",
      );
    },
  );
});
