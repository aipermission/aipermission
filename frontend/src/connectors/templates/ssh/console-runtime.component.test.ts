import { expect, it } from "vitest";
import { sshConsoleRuntime } from "./console-runtime";

it("projects only validated SSH fields without retaining opaque runtime extensions", () => {
  const input = {
    id: 7,
    connector_kind: "ssh",
    name: "Example",
    username: "root",
    host: "host.test",
    port: 22,
    target: { transfer_runtime_id: 17, opaque: "not a toolbar field" },
    opaque: { secret: "not a toolbar field" },
  };
  expect(sshConsoleRuntime(input)).toEqual({
    id: 7,
    connector_kind: "ssh",
    name: "Example",
    username: "root",
    host: "host.test",
    port: 22,
    target: { transfer_runtime_id: 17 },
  });
});

it("does not interpret other connector runtimes as SSH", () => {
  expect(sshConsoleRuntime(null)).toBeNull();
  expect(sshConsoleRuntime({ id: 8 })).toBeNull();
  expect(sshConsoleRuntime({ id: 8, connector_kind: "docker", host: { opaque: true } })).toBeNull();
  expect(sshConsoleRuntime({ id: 7, connector_kind: "ssh" })?.target).toBeUndefined();
});

it.each([0, -1, 1.5, Infinity, Number.MAX_SAFE_INTEGER + 1])("rejects invalid runtime identity %s", (id) => {
  expect(() => sshConsoleRuntime({ id, connector_kind: "ssh" })).toThrow("Invalid SSH console runtime identity.");
  expect(() => sshConsoleRuntime({ id: 7, connector_kind: "ssh", target: { transfer_runtime_id: id } })).toThrow(
    "Invalid SSH file-transfer runtime identity.",
  );
});

it("rejects invalid connector-owned public fields before rendering transfer or bulk controls", () => {
  expect(() => sshConsoleRuntime({ id: 7, connector_kind: "ssh", username: 123 })).toThrow("username");
  expect(() => sshConsoleRuntime({ id: 7, connector_kind: "ssh", host: [] })).toThrow("host");
  expect(() => sshConsoleRuntime({ id: 7, connector_kind: "ssh", port: {} })).toThrow("port");
  expect(sshConsoleRuntime({ id: 7, connector_kind: "ssh", port: "22" })?.port).toBe("22");
});
