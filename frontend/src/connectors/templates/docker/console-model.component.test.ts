import { describe, expect, it } from "vitest";
import { dockerConsoleModel } from "./console-model";

describe("Docker console presentation", () => {
  it("retains command, transport, target and scope labels independently of runtime identity", () => {
    const target = {
      ref: "docker:3:7",
      connector_kind: "docker",
      target_name: "Containers",
      profile_label: "API only",
      config: { docker_command: "sudo docker", transport_target_ref: "transport:1:2" },
    };
    expect(dockerConsoleModel.targetDisplayName({ target })).toBe("Containers");
    expect(dockerConsoleModel.targetSubtitle({ target })).toBe("sudo docker · transport:1:2");
    expect(dockerConsoleModel.targetProfileLabel({ target })).toBe("API only");
    expect(dockerConsoleModel.usesLiveConsole({ target })).toBe(true);
    expect(dockerConsoleModel.recoverableRunningActions({ target })).toEqual([]);
  });
  it("retains missing-target and default command behavior", () => {
    expect(dockerConsoleModel.targetDisplayName({})).toBe("Docker target");
    expect(dockerConsoleModel.targetProfileLabel({ target: null })).toBe("container scope");
    expect(dockerConsoleModel.targetSubtitle({ target: { ref: "docker:3:7", connector_kind: "docker" } })).toBe("docker · no transport");
  });
  it.each(["docker_command", "transport_target_ref"])("rejects malformed %s", (field) => {
    expect(() =>
      dockerConsoleModel.targetSubtitle({ target: { ref: "docker:3:7", connector_kind: "docker", config: { [field]: [] } } }),
    ).toThrow(`Invalid Docker console target ${field}.`);
  });
});
