import { expect, it } from "vitest";
import { getConsoleSessionRecovery, registerConsoleRecoveries, consoleRecoveries } from "./console-recovery-registry";
import { captureConsoleSessionRecovery } from "./_shared/console-recovery";
import { supportedConnectorKinds } from "./catalog";

it("discovers optional native recovery capabilities without exposing raw models", () => {
  expect(Object.keys(consoleRecoveries).sort()).toEqual([...supportedConnectorKinds].sort());
  expect(Object.isFrozen(consoleRecoveries)).toBe(true);
  expect(getConsoleSessionRecovery("ssh")).toHaveProperty("operationFromError");
  expect(getConsoleSessionRecovery("ssh")).not.toHaveProperty("emptyForm");
  for (const kind of supportedConnectorKinds.filter((kind) => kind !== "ssh")) expect(getConsoleSessionRecovery(kind)).toBeNull();
  for (const kind of [undefined, "missing", "constructor", "__proto__", "toString"]) expect(getConsoleSessionRecovery(kind)).toBeNull();
});

it("checks native recovery provenance, path identity, and catalog completeness", () => {
  const recovery = captureConsoleSessionRecovery({ kind: "example", recover: () => null, render: () => null });
  expect(registerConsoleRecoveries({ "./example/index.ts": recovery }, ["example"]).example).toBe(recovery);
  expect(registerConsoleRecoveries({ "./example/index.ts": null }, ["example"]).example).toBeNull();
  for (const value of [undefined, {}, { ...recovery }]) {
    expect(() => registerConsoleRecoveries({ "./example/index.ts": value }, ["example"])).toThrow("native consoleRecovery registration");
  }
  expect(() => registerConsoleRecoveries({ "./different/index.ts": recovery }, ["different"])).toThrow(
    "native consoleRecovery registration",
  );
  expect(() => registerConsoleRecoveries({}, ["example"])).toThrow("catalog/registry mismatch");
});
