import { expect, it } from "vitest";
import { getConsolePresentationModel } from "./console-model-registry";
import type { GatewayTarget } from "../../lib/gateway-contracts/core-resource-contracts";

function target(connector_kind: string): GatewayTarget {
  return {
    connector_kind,
    ref: `${connector_kind}:3:7`,
    target_id: 3,
    profile_id: 7,
    runtime_id: 19,
    target_name: "My connector",
    profile_label: "Reader",
    profile_kind: "default",
    project_id: 1,
    project_name: "My Project",
    project_slug: "my-project",
    status: "active",
    created_at: "",
    updated_at: "",
    config: {
      host: "endpoint",
      port: 22,
      transport_target_ref: "example:5:8",
      description: "Notes",
      startup_input_after_connect: "q",
      force_shell_command: "bash",
    },
    public: { username: "operator", ssh_key_id: 11 },
  };
}

it.each(["ssh", "docker", "kubernetes"])("projects native %s runtimes without requiring or synthesizing CRUD identities", (kind) => {
  const gatewayTarget = target(kind);
  const runtime = getConsolePresentationModel(kind)?.liveConsoleRuntimeTarget?.({ target: gatewayTarget });
  expect(runtime).toMatchObject({
    id: 19,
    name: "My connector",
    connector_kind: kind,
    connector_ref: gatewayTarget.ref,
    target_id: 3,
    profile_id: 7,
  });
  expect(runtime?.target).toBe(gatewayTarget);
  expect(runtime?.target).not.toHaveProperty("id");
  expect(runtime?.target).not.toHaveProperty("name");
  if (kind === "ssh") {
    expect(runtime).toMatchObject({
      host: "endpoint",
      port: 22,
      username: "operator",
      ssh_key_id: 11,
      description: "Notes",
      startup_input_after_connect: "q",
      force_shell_command: "bash",
    });
  } else {
    expect(runtime).toMatchObject({ host: "example:5:8", port: 0, username: "Reader" });
  }
});

it.each(["ssh", "docker", "kubernetes"])("rejects malformed %s runtime fields at its native boundary", (kind) => {
  const gatewayTarget = target(kind);
  const field = kind === "ssh" ? "host" : "transport_target_ref";
  expect(() =>
    getConsolePresentationModel(kind)?.liveConsoleRuntimeTarget?.({ target: { ...gatewayTarget, config: { [field]: [] } } }),
  ).toThrow("console target");
});

it.each(["postgres", "clickhouse", "redis", "rabbitmq", "kafka", "mail", "s3"])("does not invent a runtime projection for %s", (kind) => {
  expect(getConsolePresentationModel(kind)).not.toHaveProperty("liveConsoleRuntimeTarget");
  expect(getConsolePresentationModel(kind)).not.toHaveProperty("save");
});
