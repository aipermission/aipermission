import type { SSHDockerContainer } from "./model-types";
import type { SSHDockerResponse } from "./operation-types";

export function sshDockerResponse(value: unknown): SSHDockerResponse {
  if (
    !record(value) ||
    typeof value.ok !== "boolean" ||
    (value.available !== undefined && typeof value.available !== "boolean") ||
    ["stdout", "stderr"].some((field) => value[field] !== undefined && typeof value[field] !== "string") ||
    (value.exit_code !== undefined && (typeof value.exit_code !== "number" || !Number.isSafeInteger(value.exit_code))) ||
    (value.duration_ms !== undefined &&
      (typeof value.duration_ms !== "number" || !Number.isFinite(value.duration_ms) || value.duration_ms < 0)) ||
    (value.containers !== undefined && (!Array.isArray(value.containers) || !value.containers.every(validContainer)))
  ) {
    throw new Error("Invalid SSH Docker operation response.");
  }
  return value as SSHDockerResponse;
}

function validContainer(value: unknown): value is SSHDockerContainer {
  return (
    record(value) &&
    [
      "id",
      "name",
      "status",
      "state",
      "image",
      "ports",
      "running_for",
      "created_at",
      "size",
      "command",
      "networks",
      "mounts",
      "labels",
    ].every((field) => value[field] === undefined || typeof value[field] === "string")
  );
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
