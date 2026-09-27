import { describe, expect, it } from "vitest";
import { sshConsoleModel } from "./console-model";

describe("SSH console presentation", () => {
  it("retains target and public profile labels independently of persistence/runtime IDs", () => {
    const target = {
      ref: "ssh:3:7",
      connector_kind: "ssh",
      target_name: "Shell",
      profile_label: "Admin",
      config: { host: "host.local", port: "2222" },
      public: { username: "root" },
    };
    expect(sshConsoleModel.targetDisplayName({ target })).toBe("Shell");
    expect(sshConsoleModel.targetSubtitle({ target })).toBe("root@host.local:2222");
    expect(sshConsoleModel.targetProfileLabel({ target })).toBe("Admin");
    expect(sshConsoleModel.usesLiveConsole({ target })).toBe(true);
    expect(sshConsoleModel.recoverableRunningActions({ target })).toEqual(["exec"]);
  });
  it("retains missing-target, profile and endpoint defaults", () => {
    expect(sshConsoleModel.targetDisplayName({})).toBe("SSH target");
    expect(sshConsoleModel.targetProfileLabel({ target: null })).toBe("terminal");
    const target = { ref: "ssh:3:7", connector_kind: "ssh" };
    expect(sshConsoleModel.targetSubtitle({ target })).toBe("ssh@host:22");
    expect(sshConsoleModel.targetSubtitle({ target, runtimeTarget: null })).toBe("ssh@host:22");
    expect(sshConsoleModel.targetProfileLabel({ target: { ...target, public: { username: "reader" } } })).toBe("reader");
  });
  it("uses validated live runtime fallbacks only when target fields are absent", () => {
    const target = { ref: "ssh:3:7", connector_kind: "ssh" };
    const runtimeTarget = { id: 91, name: "Runtime", username: "reader", host: "live.local", port: 2222 };
    expect(sshConsoleModel.targetSubtitle({ target, runtimeTarget })).toBe("reader@live.local:2222");
    expect(
      sshConsoleModel.targetSubtitle({
        target: { ...target, public: { username: "admin" }, config: { host: "target.local", port: 22 } },
        runtimeTarget,
      }),
    ).toBe("admin@target.local:22");
  });
  it("rejects malformed native target and runtime fields", () => {
    const target = { ref: "ssh:3:7", connector_kind: "ssh" };
    expect(() => sshConsoleModel.targetSubtitle({ target: { ...target, config: { port: [] } } })).toThrow(
      "Invalid SSH console target port.",
    );
    expect(() => sshConsoleModel.targetProfileLabel({ target: { ...target, public: { username: false } } })).toThrow(
      "Invalid SSH console target username.",
    );
    expect(() => sshConsoleModel.targetSubtitle({ target, runtimeTarget: { id: 91, name: "Runtime", host: 123 } })).toThrow(
      "Invalid SSH console target host.",
    );
  });
});
