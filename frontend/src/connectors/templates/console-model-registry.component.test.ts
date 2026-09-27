import { expect, it } from "vitest";
import { consoleModels, getConsolePresentationModel, registerConsoleModels } from "./console-model-registry";
import { captureConsolePresentation } from "./_shared/console-presentation";
import { supportedConnectorKinds } from "./catalog";

function registration(kind = "example") {
  return captureConsolePresentation({
    kind,
    decodeTarget: (target) => ({ name: target.name }),
    model: {
      targetDisplayName: ({ target }) => target?.name || "Example",
      targetSubtitle: () => "Endpoint",
      targetProfileLabel: () => "Profile",
      usesLiveConsole: () => false,
      recoverableRunningActions: () => [],
    },
  });
}

it("discovers every native presentation without exposing form or credential mutation models", () => {
  expect(Object.keys(consoleModels).sort()).toEqual([...supportedConnectorKinds].sort());
  expect(Object.isFrozen(consoleModels)).toBe(true);
  for (const [kind, model] of Object.entries(consoleModels)) {
    expect(model.kind).toBe(kind);
    expect(getConsolePresentationModel(kind)).toBe(model);
    expect(model).not.toHaveProperty("emptyForm");
    expect(model).not.toHaveProperty("saveCredential");
    expect(Object.isFrozen(model)).toBe(true);
  }
  expect(getConsolePresentationModel(undefined)).toBeNull();
  expect(getConsolePresentationModel("missing")).toBeNull();
});

it("rejects missing, forged and path-mismatched native presentation models", () => {
  for (const candidate of [undefined, null, {}, { ...registration() }, registration("different")]) {
    expect(() => registerConsoleModels({ "./example/index.ts": candidate }, ["example"])).toThrow("native consoleModel registration");
  }
  expect(() => registerConsoleModels({ "./bad.ts": registration() }, ["example"])).toThrow("Invalid connector template path");
});

it.each(["constructor", "toString", "__proto__", "hasOwnProperty"])("does not expose inherited registry member %s", (kind) => {
  expect(getConsolePresentationModel(kind)).toBeNull();
});

it("fails closed when console models and catalog differ", () => {
  expect(() => registerConsoleModels({}, ["example"])).toThrow("catalog/registry mismatch");
  expect(() => registerConsoleModels({ "./example/index.ts": registration() }, [])).toThrow("catalog/registry mismatch");
});

it("captures a future native presentation without concrete-kind switches", () => {
  const models = registerConsoleModels({ "./example/index.ts": registration() }, ["example"]);
  expect(models.example.targetDisplayName({ target: { ref: "example:3:7", connector_kind: "example", name: "Future" } })).toBe("Future");
});
