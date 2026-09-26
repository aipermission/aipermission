import type { DockerResource } from "./resource-types";

const stringFields: readonly (keyof DockerResource)[] = ["id", "name", "repository", "tag", "digest", "image", "compose_project", "compose_service", "size", "driver", "scope", "mountpoint", "status", "health", "state", "created_since", "created_at", "labels", "ports", "ipv6", "internal"];

export function dockerOutputRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? Object.fromEntries(Object.entries(value)) : {};
}

function isResource(value: unknown): value is DockerResource {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const fields = dockerOutputRecord(value);
  return stringFields.every((key) => fields[key] === undefined || typeof fields[key] === "string") &&
    (fields.containers === undefined || (typeof fields.containers === "number" && Number.isFinite(fields.containers)));
}

export function dockerOutputResources(output: unknown, kind: string): DockerResource[] {
  const rows = dockerOutputRecord(output)[kind];
  return Array.isArray(rows) ? rows.filter(isResource) : [];
}
