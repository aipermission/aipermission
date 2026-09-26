const safePathSegment = /^[A-Za-z_][A-Za-z0-9_]*$/;
const forbiddenPathSegments = new Set(["__proto__", "constructor", "prototype"]);

export type NetworkTransportDescriptor = {
  mode: string;
  label: string;
  option_label: string;
  profile_label: string;
  profile_endpoint: { fields: { path: string; fallback: string | number }[]; separator: string };
};

export function assertNetworkTransportMetadata(kind: string, transport: unknown): asserts transport is NetworkTransportDescriptor | undefined {
  if (transport === undefined) return;
  if (!isPlainObject(transport)) {
    throw new Error(`Connector template ${kind} metadata network_transport must be an object`);
  }
  for (const field of ["mode", "label", "option_label", "profile_label"]) {
    if (!isNonEmptyString(transport[field])) {
      throw new Error(`Connector template ${kind} metadata network_transport requires mode, label, option_label, and profile_label`);
    }
  }
  const endpoint = transport.profile_endpoint;
  if (
    !isPlainObject(endpoint) ||
    !Array.isArray(endpoint.fields) ||
    endpoint.fields.length === 0 ||
    !isNonEmptyString(endpoint.separator)
  ) {
    throw new Error(`Connector template ${kind} metadata network_transport requires a profile_endpoint field template`);
  }
  endpoint.fields.forEach((field: unknown, index: number) => assertEndpointField(kind, field, index));
}

export function uniqueNetworkTransportDescriptors(
  entries: Iterable<[string, { network_transport?: unknown }]>,
): NetworkTransportDescriptor[] {
  const byMode = new Map<string, { kind: string; transport: NetworkTransportDescriptor }>();
  for (const [kind, metadata] of entries) {
    const transport = metadata?.network_transport;
    if (!transport) continue;
    assertNetworkTransportMetadata(kind, transport);
    const existing = byMode.get(transport.mode);
    if (existing && stableJSON(existing.transport) !== stableJSON(transport)) {
      throw new Error(`Connector templates ${existing.kind} and ${kind} declare conflicting network_transport mode ${transport.mode}`);
    }
    if (!existing) byMode.set(transport.mode, { kind, transport });
  }
  return [...byMode.values()].map(({ transport }) => transport);
}

export function publicEndpointValue(value: unknown, path: unknown): unknown {
  if (!isPublicEndpointPath(path)) return undefined;
  return path.split(".").reduce<unknown>((current, key) => (isPlainObject(current) ? current[key] : undefined), value);
}

function assertEndpointField(kind: string, field: unknown, index: number): void {
  if (!isPlainObject(field) || !isPublicEndpointPath(field.path)) {
    throw new Error(`Connector template ${kind} metadata network_transport profile_endpoint field ${index} has an invalid public path`);
  }
  if (!Object.hasOwn(field, "fallback") || !isDisplayScalar(field.fallback)) {
    throw new Error(`Connector template ${kind} metadata network_transport profile_endpoint field ${index} requires a display fallback`);
  }
}

function isPublicEndpointPath(path: unknown): path is string {
  if (!isNonEmptyString(path)) return false;
  const segments = path.split(".");
  if (segments.some((segment) => !safePathSegment.test(segment) || forbiddenPathSegments.has(segment))) return false;
  if (segments[0] === "target") {
    return segments.length === 2 ? segments[1] === "name" : segments.length > 2 && segments[1] === "config";
  }
  if (segments[0] === "profile") {
    return segments.length === 2 ? segments[1] === "label" : segments.length > 2 && segments[1] === "public";
  }
  return false;
}

function isDisplayScalar(value: unknown): value is string | number {
  return (typeof value === "string" && value.trim().length > 0) || (typeof value === "number" && Number.isFinite(value));
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.trim().length > 0;
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const prototype = Object.getPrototypeOf(value);
  return prototype === Object.prototype || prototype === null;
}

function stableJSON(value: unknown): string | undefined {
  if (Array.isArray(value)) return `[${value.map(stableJSON).join(",")}]`;
  if (isPlainObject(value)) {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${stableJSON(value[key])}`)
      .join(",")}}`;
  }
  return JSON.stringify(value);
}
