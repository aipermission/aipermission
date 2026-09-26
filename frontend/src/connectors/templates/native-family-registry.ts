import { connectorKindFromPath } from "./template-registration";

export function registerNativeFamilies<Family>(
  modules: Record<string, unknown>,
  expectedKinds: readonly string[],
  label: string,
  capture: (_kind: string, _registration: unknown) => Family,
): Readonly<Record<string, Family>> {
  const entries: [string, Family][] = Object.entries(modules).map(([path, registration]) => {
    const kind = connectorKindFromPath(path);
    return [kind, capture(kind, registration)];
  });
  entries.sort(([left], [right]) => left.localeCompare(right));
  const actual = entries.map(([kind]) => kind);
  const expected = [...expectedKinds].sort();
  if (actual.length !== expected.length || actual.some((kind, index) => kind !== expected[index])) {
    throw new Error(`${label} catalog/registry mismatch. catalog=${expected.join(",")} registry=${actual.join(",")}`);
  }
  return Object.freeze(Object.fromEntries(entries));
}
