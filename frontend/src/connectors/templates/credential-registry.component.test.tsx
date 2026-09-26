import { expect, it } from "vitest";
import { defineCredentialFamily } from "./_shared/credential-family-registration";
import { credentialFamilies, registerCredentialFamilies } from "./credential-registry";
import { supportedConnectorKinds } from "./catalog";

function registration(kind = "example") {
  return defineCredentialFamily<{}, { connector_kind: string }, {}, "create">({
    kind,
    label: "Example",
    decodeTargets: () => [],
    emptyState: () => ({}),
    model: {},
    rows: () => [],
    displayRow: () => ({
      row_id: "example:1:2",
      connector_kind: kind,
      connector_label: "Example",
      name: "Test",
      kind: "identity",
      target_label: "Test",
      metadata: [],
    }),
    renderForm: () => null,
  });
}

it("discovers every catalog family as a captured UI surface", () => {
  expect(Object.keys(credentialFamilies).sort()).toEqual([...supportedConnectorKinds].sort());
  expect(Object.isFrozen(credentialFamilies)).toBe(true);
  for (const [kind, family] of Object.entries(credentialFamilies)) {
    expect(family.kind).toBe(kind);
    expect(typeof family.Rows).toBe("function");
    expect(family).not.toHaveProperty("model");
  }
});

it("rejects missing, forged, or mismatched native registrations", () => {
  for (const candidate of [undefined, null, { kind: "example", create: () => ({}) }, { ...registration() }, registration("different")]) {
    expect(() => registerCredentialFamilies({ "./example/index.ts": candidate }, ["example"])).toThrow(
      "native credentialFamily registration",
    );
  }
  expect(() => registerCredentialFamilies({ "./bad.ts": registration() }, ["example"])).toThrow("Invalid connector template path");
});

it("fails closed when the family registry and catalog differ", () => {
  expect(() => registerCredentialFamilies({}, ["example"])).toThrow("catalog/registry mismatch");
  expect(() => registerCredentialFamilies({ "./example/index.ts": registration() }, [])).toThrow("catalog/registry mismatch");
});

it("registers a future native family without concrete-kind changes to the host", () => {
  const registered = registerCredentialFamilies({ "./example/index.ts": registration() }, ["example"]);
  expect(registered.example.kind).toBe("example");
  expect(Object.isFrozen(registered.example)).toBe(true);
});
