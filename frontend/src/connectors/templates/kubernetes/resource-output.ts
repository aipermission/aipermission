import type { KubernetesResource } from "./resource-types";

const stringFields: readonly (keyof KubernetesResource)[] = [
  "namespace", "name", "kind", "object", "reason", "last_timestamp", "message", "type", "node", "ready", "image",
  "cluster_ip", "ports", "hosts", "class", "roles", "version", "phase", "age", "external_ip",
];
const numberFields: readonly (keyof KubernetesResource)[] = ["count", "restarts"];

function isResource(value: unknown): value is KubernetesResource {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const fields: Record<string, unknown> = Object.fromEntries(Object.entries(value));
  return stringFields.every((key) => fields[key] === undefined || typeof fields[key] === "string") &&
    numberFields.every((key) => fields[key] === undefined || (typeof fields[key] === "number" && Number.isFinite(fields[key])));
}

export function kubernetesOutputField(output: unknown, field: string): unknown {
  return output && typeof output === "object" && !Array.isArray(output) && field in output
    ? Reflect.get(output, field)
    : undefined;
}

export function kubernetesOutputResources(output: unknown, field: string): KubernetesResource[] {
  const value = kubernetesOutputField(output, field);
  return Array.isArray(value) ? value.filter(isResource) : [];
}

export function kubernetesOutputNamespaces(output: unknown): { name: string }[] {
  const value = kubernetesOutputField(output, "namespaces");
  if (!Array.isArray(value)) return [];
  return value.filter((entry): entry is { name: string } => !!entry && typeof entry === "object" && !Array.isArray(entry) && typeof entry.name === "string");
}
