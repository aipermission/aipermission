import { describe, expect, it } from "vitest";
import { s3ConsoleModel } from "./console-model";
import { s3ConsoleTarget } from "./console-target";

describe("S3 console presentation", () => {
  it("preserves bucket/endpoint/profile labels without persistence identity", () => {
    const target = {
      ref: "s3:3:7",
      connector_kind: "s3",
      target_name: "Assets",
      profile_label: "Reader",
      config: { scheme: "http", host: "store.local", port: "9000", bucket: "assets", connection_mode: "over_ssh" },
    };
    expect(s3ConsoleModel.targetDisplayName({ target })).toBe("Assets");
    expect(s3ConsoleModel.targetSubtitle({ target })).toBe("http://store.local:9000/assets · over ssh");
    expect(s3ConsoleModel.targetProfileLabel({ target })).toBe("Reader");
    expect(s3ConsoleModel.usesLiveConsole({ target })).toBe(false);
    expect(s3ConsoleModel.recoverableRunningActions({ target })).toEqual([]);
  });

  it("retains missing-target and default endpoint behavior", () => {
    expect(s3ConsoleModel.targetDisplayName({})).toBe("S3 target");
    expect(s3ConsoleModel.targetProfileLabel({ target: null })).toBe("default");
    expect(s3ConsoleModel.targetSubtitle({ target: { ref: "s3:3:7", connector_kind: "s3" } })).toBe(
      "https://s3.amazonaws.com:443/bucket · direct",
    );
  });

  it("preserves the existing transfer runtime and validates only native config", () => {
    const target = {
      ref: "s3:3:7",
      connector_kind: "s3",
      name: "Store",
      transfer_runtime_id: 91,
      config: { transport_target_ref: "transport:1:2", trust_conditional_requests: true, opaque: true },
    };
    expect(s3ConsoleTarget(target)).toStrictEqual({
      ref: "s3:3:7",
      name: "Store",
      target_name: undefined,
      transfer_runtime_id: 91,
      config: {
        bucket: undefined,
        scheme: undefined,
        host: undefined,
        port: undefined,
        connection_mode: undefined,
        transport_target_ref: "transport:1:2",
        trust_conditional_requests: true,
      },
    });
    expect(s3ConsoleTarget({ ...target, transfer_runtime_id: null }).transfer_runtime_id).toBeNull();
    expect(s3ConsoleTarget({ ref: "s3:3:7", connector_kind: "s3" }).transfer_runtime_id).toBeUndefined();
  });

  it.each([{ port: [] }, { bucket: 123 }, { trust_conditional_requests: "true" }, { transport_target_ref: false }])(
    "rejects malformed config %j",
    (config) => {
      expect(() => s3ConsoleModel.targetSubtitle({ target: { ref: "s3:3:7", connector_kind: "s3", config } })).toThrow(
        "Invalid S3 console target",
      );
    },
  );
});
