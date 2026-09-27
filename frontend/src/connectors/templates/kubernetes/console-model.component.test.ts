import { describe, expect, it } from "vitest";
import { kubernetesConsoleModel } from "./console-model";

describe("Kubernetes console presentation", () => {
  it("retains command, transport, context, namespace and scope independently of runtime identity", () => {
    const target = {
      ref: "kubernetes:3:7",
      connector_kind: "kubernetes",
      target_name: "Cluster",
      profile_label: "Dev only",
      config: { kubectl_command: "sudo kubectl", transport_target_ref: "transport:1:2", context: "dev", default_namespace: "app" },
    };
    expect(kubernetesConsoleModel.targetDisplayName({ target })).toBe("Cluster");
    expect(kubernetesConsoleModel.targetSubtitle({ target })).toBe("sudo kubectl · transport:1:2 · context dev · ns app");
    expect(kubernetesConsoleModel.targetProfileLabel({ target })).toBe("Dev only");
    expect(kubernetesConsoleModel.usesLiveConsole({ target })).toBe(true);
    expect(kubernetesConsoleModel.recoverableRunningActions({ target })).toEqual([]);
  });
  it("retains missing-target and default command behavior", () => {
    expect(kubernetesConsoleModel.targetDisplayName({})).toBe("Kubernetes target");
    expect(kubernetesConsoleModel.targetProfileLabel({ target: null })).toBe("namespace scope");
    expect(kubernetesConsoleModel.targetSubtitle({ target: { ref: "kubernetes:3:7", connector_kind: "kubernetes" } })).toBe(
      "kubectl · no transport",
    );
  });
  it.each(["kubectl_command", "transport_target_ref", "context", "default_namespace"])("rejects malformed %s", (field) => {
    expect(() =>
      kubernetesConsoleModel.targetSubtitle({
        target: { ref: "kubernetes:3:7", connector_kind: "kubernetes", config: { [field]: false } },
      }),
    ).toThrow(`Invalid Kubernetes console target ${field}.`);
  });
});
