import { optionalConsolePort, optionalConsoleText } from "../_shared/console-target-config";
import type { SSHConsoleRuntime, SSHConsoleRuntimeInput } from "./console-types";

export function sshConsoleRuntime(value: SSHConsoleRuntimeInput | null): SSHConsoleRuntime | null {
  if (!value || value.connector_kind !== "ssh") return null;
  if (!Number.isSafeInteger(value.id) || value.id <= 0) throw new Error("Invalid SSH console runtime identity.");
  const transferID = value.target?.transfer_runtime_id;
  if (transferID !== undefined && (!Number.isSafeInteger(transferID) || transferID <= 0)) {
    throw new Error("Invalid SSH file-transfer runtime identity.");
  }
  return {
    id: value.id,
    connector_kind: value.connector_kind,
    name: value.name,
    username: optionalConsoleText(value.username, "SSH", "username"),
    host: optionalConsoleText(value.host, "SSH", "host"),
    port: optionalConsolePort(value.port, "SSH"),
    target: transferID === undefined ? undefined : { transfer_runtime_id: transferID },
  };
}
