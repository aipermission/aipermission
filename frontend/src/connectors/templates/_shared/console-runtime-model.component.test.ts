import { expect, expectTypeOf, it, vi } from "vitest";
import { captureConsoleRuntimeProjection } from "./console-runtime-model";
import type { GatewayTarget } from "../../../lib/gateway-contracts/core-resource-contracts";

const target: GatewayTarget = {
  connector_kind: "example",
  ref: "example:3:7",
  target_id: 3,
  profile_id: 7,
  runtime_id: 19,
  target_name: "My connector",
  profile_label: "Reader",
  profile_kind: "default",
  project_id: 1,
  project_name: "My Project",
  project_slug: "my-project",
  status: "active",
  created_at: "",
  updated_at: "",
};

it("requires native runtime provenance fields to satisfy the shared return types", () => {
  type AcceptedRuntime = ReturnType<Parameters<typeof captureConsoleRuntimeProjection>[1]>;
  type Identity = { id: number; name: string };
  expectTypeOf<Identity & { connector_kind: number }>().not.toExtend<AcceptedRuntime>();
  expectTypeOf<Identity & { connector_ref: number }>().not.toExtend<AcceptedRuntime>();
  expectTypeOf<Identity & { target_id: string }>().not.toExtend<AcceptedRuntime>();
  expectTypeOf<Identity & { profile_id: number[] }>().not.toExtend<AcceptedRuntime>();
  expectTypeOf<
    Identity & { connector_kind: string; connector_ref: string; target_id: number; profile_id: number }
  >().toExtend<AcceptedRuntime>();
});

it("preserves the original gateway target and the independent runtime identity", () => {
  const decode = vi.fn((target: GatewayTarget & { runtime_id: number }) => ({ runtime: target.runtime_id, label: target.target_name }));
  const project = vi.fn(({ target }: { target: { runtime: number; label: string } }) => ({
    id: target.runtime,
    name: target.label,
    host: "endpoint",
    connector_ref: "example:3:7",
  }));
  const projection = captureConsoleRuntimeProjection(decode, project);
  const runtime = projection({ target });
  expect(runtime).toEqual({ id: 19, name: "My connector", host: "endpoint", connector_ref: target.ref, target });
  expect(runtime.target).toBe(target);
  expect(decode).toHaveBeenCalledWith(target);
  expect(project).toHaveBeenCalledWith({ target: { runtime: 19, label: "My connector" } });
  expect(target).not.toHaveProperty("id");
});

it.each([undefined, 0, -1, 1.5, NaN, Infinity])("rejects invalid runtime identity %s before native projection", (runtime_id) => {
  const decode = vi.fn();
  const project = vi.fn(() => ({ id: 19, name: "Example" }));
  expect(() => captureConsoleRuntimeProjection(decode, project)({ target: { ...target, runtime_id } })).toThrow("valid runtime identity");
  expect(decode).not.toHaveBeenCalled();
  expect(project).not.toHaveBeenCalled();
});

it.each([undefined, "19", 3, 7, 0])("rejects a substituted native runtime identity %s", (id) => {
  const projection = captureConsoleRuntimeProjection(
    (target) => target,
    () => ({ id, name: "Example" }),
  );
  expect(() => projection({ target })).toThrow("preserve the runtime identity");
});

it("propagates native decoding and projection failures", () => {
  const failedDecode = captureConsoleRuntimeProjection(
    () => {
      throw new Error("native config");
    },
    () => ({ id: 19, name: "Example" }),
  );
  expect(() => failedDecode({ target })).toThrow("native config");
  const failedProject = captureConsoleRuntimeProjection(
    (target) => target,
    () => {
      throw new Error("native projection");
    },
  );
  expect(() => failedProject({ target })).toThrow("native projection");
});
