import { expect, it } from "vitest";
import { defineConnectorFamily } from "./_shared/connector-family-registration";
import { connectorFamilies, registerConnectorFamilies } from "./connector-family-registry";
import { supportedConnectorKinds } from "./catalog";

function registration(kind = "example") {
  return defineConnectorFamily<
    { connector_kind: string },
    { id: number },
    { id: number; connector_kind: string },
    never,
    never,
    { open: boolean }
  >({
    kind,
    decodeTargets: () => [],
    decodeCredentials: () => [],
    emptyForm: () => ({ connector_kind: kind }),
    emptyOperation: () => ({ open: false }),
    model: { test: async () => ({ ok: true }), activeCredential: () => null, submitLabel: () => "Create" },
    tableModel: { targetEndpoint: () => "Example", credentialHint: () => null, canEdit: () => true, canDelete: () => true },
    deleteDialog: () => ({}),
    renderForm: () => null,
    renderRowActions: () => null,
    renderOperations: () => null,
  });
}

it("discovers every connector as a captured provider and typed inventory projection", () => {
  expect(Object.keys(connectorFamilies).sort()).toEqual([...supportedConnectorKinds].sort());
  expect(Object.isFrozen(connectorFamilies)).toBe(true);
  for (const [kind, family] of Object.entries(connectorFamilies)) {
    expect(family.kind).toBe(kind);
    expect(typeof family.Provider).toBe("function");
    expect(typeof family.tableTemplate.model.targetEndpoint).toBe("function");
    expect(family).not.toHaveProperty("model");
    expect(family).not.toHaveProperty("emptyForm");
  }
});

it("rejects missing, forged, or path-mismatched native connector registrations", () => {
  for (const candidate of [undefined, null, { kind: "example", create: () => ({}) }, { ...registration() }, registration("different")]) {
    expect(() => registerConnectorFamilies({ "./example/index.ts": candidate }, ["example"])).toThrow(
      "native connectorFamily registration",
    );
  }
  expect(() => registerConnectorFamilies({ "./bad.ts": registration() }, ["example"])).toThrow("Invalid connector template path");
});

it("fails closed when the connector registrations and catalog differ", () => {
  expect(() => registerConnectorFamilies({}, ["example"])).toThrow("catalog/registry mismatch");
  expect(() => registerConnectorFamilies({ "./example/index.ts": registration() }, [])).toThrow("catalog/registry mismatch");
});

it("captures a future native connector without concrete-kind switches in the host", () => {
  const registered = registerConnectorFamilies({ "./example/index.ts": registration() }, ["example"]);
  expect(registered.example.kind).toBe("example");
  expect(Object.isFrozen(registered.example)).toBe(true);
});
