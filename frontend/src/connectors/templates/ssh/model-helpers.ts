import type { SSHForm } from "./form-types";
import type { SSHCredentialResource, SSHHostKeyError, SSHPayload } from "./model-types";

export function payloadFromForm(form: SSHForm): SSHPayload {
  return {
    name: form.name,
    host: form.host,
    port: Number(form.port),
    username: form.username,
    ssh_key_id: Number(form.ssh_key_id),
    profile_id: Number(form.profile_id || 0),
    description: form.description,
    startup_input_after_connect: form.startup_input_after_connect,
    force_shell_command: form.force_shell_command,
  };
}

export function targetConfigFromPayload(payload: SSHPayload) {
  return {
    host: payload.host,
    port: payload.port,
    description: payload.description,
    startup_input_after_connect: payload.startup_input_after_connect,
    force_shell_command: payload.force_shell_command,
  };
}

export function profilePublicFromPayload(payload: SSHPayload) {
  return {
    username: payload.username,
    ssh_key_id: payload.ssh_key_id,
  };
}

export function isHostKeyError(error: unknown): error is SSHHostKeyError {
  if (!error || typeof error !== "object" || !("status" in error) || !("data" in error)) return false;
  const data = error.data;
  if (!data || typeof data !== "object" || !("code" in data) || !("host_key" in data)) return false;
  const hostKey = data.host_key;
  if (!hostKey || typeof hostKey !== "object" || Array.isArray(hostKey)) return false;
  const key = hostKey as Record<string, unknown>;
  return (
    error.status === 409 &&
    typeof data.code === "string" &&
    ["unknown_ssh_host_key", "changed_ssh_host_key"].includes(data.code) &&
    ["host", "hostname", "public_key", "fingerprint_sha256", "key_type"].every(
      (field) => typeof key[field] === "string" && key[field] !== "",
    ) &&
    typeof key.port === "number" &&
    Number.isInteger(key.port) &&
    key.port > 0 &&
    key.port <= 65535 &&
    (key.changed === undefined || typeof key.changed === "boolean") &&
    (key.existing_fingerprints === undefined ||
      (Array.isArray(key.existing_fingerprints) && key.existing_fingerprints.every((item: unknown) => typeof item === "string")))
  );
}

export function keyNameFromFilename(filename: string, fallback: string) {
  return (
    filename
      .replace(/\.[^.]+$/, "")
      .replace(/[^a-zA-Z0-9_. -]+/g, "-")
      .trim()
      .slice(0, 80) || fallback
  );
}

export function sshCredentialResourcesResponse(value: unknown): SSHCredentialResource[] {
  const items = value && typeof value === "object" && "items" in value ? value.items : value;
  if (!Array.isArray(items) || !items.every(validCredential)) throw new Error("Invalid SSH credential resources.");
  return items.map((item) => ({ ...item, connector_kind: "ssh", resource_kind: "ssh_key", resource_ref: `ssh:ssh_key:${item.id}` }));
}

function validCredential(value: unknown): value is SSHCredentialResource {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const row = value as Record<string, unknown>;
  return (
    typeof row.id === "number" &&
    Number.isSafeInteger(row.id) &&
    row.id > 0 &&
    typeof row.name === "string" &&
    typeof row.key_type === "string" &&
    ["fingerprint", "install_command", "resource_kind", "resource_ref", "connector_kind"].every(
      (field) => row[field] === undefined || typeof row[field] === "string",
    )
  );
}
