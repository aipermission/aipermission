import { describe, expect, it } from "vitest";
import { getConnectorMetadata } from "./catalog";
import redisTemplate from "./redis";
import { assertConnectorTemplate, connectorKindFromPath, requiredModelFunctions } from "./template-registration";

function registeredTemplate() {
  return { ...redisTemplate, metadata: getConnectorMetadata("redis") };
}

describe("connector template registration validation", () => {
  it("accepts the connector-owned standard lifecycle without replacing model identity", () => {
    const template = registeredTemplate();
    expect(() => assertConnectorTemplate("redis", template)).not.toThrow();
    expect(template.model).toBe(redisTemplate.model);
  });

  it("rejects missing metadata and non-object registrations", () => {
    for (const template of [null, undefined, [], false, {}, { metadata: [] }]) {
      expect(() => assertConnectorTemplate("redis", template)).toThrow("is missing metadata");
    }
  });

  it("checks metadata identity and does not coerce missing display strings", () => {
    const template = registeredTemplate();
    expect(() => assertConnectorTemplate("other", template)).toThrow("metadata kind must be other");
    for (const field of ["label", "summary", "version"]) {
      for (const value of ["", "  ", null, 42]) {
        expect(() => assertConnectorTemplate("redis", { ...template, metadata: { ...template.metadata, [field]: value } })).toThrow(
          `metadata is missing ${field}`,
        );
      }
    }
  });

  it("rejects unknown icons, lifecycle modes, and malformed network transport metadata", () => {
    const template = registeredTemplate();
    for (const icon of [null, "unknown"]) {
      expect(() => assertConnectorTemplate("redis", { ...template, metadata: { ...template.metadata, icon } })).toThrow("metadata icon");
    }
    expect(() =>
      assertConnectorTemplate("redis", { ...template, metadata: { ...template.metadata, profile_lifecycle: "unknown" } }),
    ).toThrow("profile_lifecycle must be standard or custom");
    expect(() => assertConnectorTemplate("redis", { ...template, metadata: { ...template.metadata, network_transport: [] } })).toThrow(
      "network_transport must be an object",
    );
  });

  it("requires every native slot and model function", () => {
    const template = registeredTemplate();
    for (const slot of ["Console", "CredentialForm", "Form", "RowActions"]) {
      expect(() => assertConnectorTemplate("redis", { ...template, [slot]: null })).toThrow(`is missing ${slot} slot`);
    }
    for (const model of [null, [], "model"]) {
      expect(() => assertConnectorTemplate("redis", { ...template, model })).toThrow("is missing model exports");
    }
    for (const fn of requiredModelFunctions) {
      expect(() => assertConnectorTemplate("redis", { ...template, model: { ...template.model, [fn]: null } })).toThrow(
        `model is missing ${fn}()`,
      );
    }
  });

  it("rejects a standard lifecycle lookalike but permits an explicitly custom lifecycle", () => {
    const template = registeredTemplate();
    const model = { ...template.model, save: async () => {} };
    expect(() => assertConnectorTemplate("redis", { ...template, model })).toThrow("must use the shared executable contract");
    expect(() =>
      assertConnectorTemplate("redis", { ...template, model, metadata: { ...template.metadata, profile_lifecycle: "custom" } }),
    ).not.toThrow();
  });

  it("extracts kinds only from connector entry module paths", () => {
    expect(connectorKindFromPath("./example/index.ts")).toBe("example");
    for (const path of ["./example/index.jsx", "./example/extra/index.ts", "example/index.ts", "./index.ts"]) {
      expect(() => connectorKindFromPath(path)).toThrow("Invalid connector template path");
    }
  });
});
