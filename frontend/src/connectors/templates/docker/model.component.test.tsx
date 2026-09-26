import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  credentialRows,
  credentialStateFromRow,
  emptyForm,
  formFromTarget,
  liveConsoleRuntimeTarget,
  save,
  saveCredential,
  submitDisabled,
  syncForm,
} from "./model";
import type { DockerTarget } from "./form-types";

const api = vi.hoisted(() => ({ post: vi.fn(), put: vi.fn() }));
vi.mock("../../../lib/api.ts", () => ({ apiPost: api.post, apiPut: api.put, apiDelete: vi.fn() }));

describe("Docker scoped credential model", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.post.mockResolvedValue({ id: 3, profiles: [] });
    api.put.mockResolvedValue({ id: 3, profiles: [] });
  });

  it("keeps runtime identity independent of profile identity", () => {
    const target = {
      runtime_id: "opaque-runtime-91",
      ref: "docker:3:9",
      target_id: 3,
      profile_id: 9,
      name: "engine",
      profile_label: "api-only",
      config: { transport_target_ref: "ssh:1:2" },
    };
    expect(liveConsoleRuntimeTarget({ target })).toMatchObject({
      id: "opaque-runtime-91",
      target_id: 3,
      profile_id: 9,
      username: "api-only",
      target,
    });
  });

  it("persists only the selected container scope through the shared atomic route", async () => {
    const form = {
      ...emptyForm(),
      transport_target_ref: "ssh:1:2",
      scope_mode: "selected",
      allowed_containers: "api",
      allowed_patterns: "worker-*",
    };
    await save({ mode: "create", form });
    expect(api.post).toHaveBeenCalledWith(
      "/api/connector-targets/with-profile",
      expect.objectContaining({
        target: {
          connector_kind: "docker",
          name: "docker-host",
          project_id: 0,
          config: { connection_mode: "over_ssh", transport_target_ref: "ssh:1:2", docker_command: "docker" },
        },
        profile: {
          kind: "container_scope",
          label: "all-containers",
          public: { scope_mode: "selected", allowed_containers: "api", allowed_patterns: "worker-*" },
          secret: {},
          risk_label: "container access",
        },
      }),
    );
    expect(submitDisabled({ state: { state: "idle" }, form: emptyForm() })).toBe(true);
    expect(submitDisabled({ state: { state: "idle" }, form })).toBe(false);
    expect(syncForm({ form: { ...form, docker_command: "" } }).docker_command).toBe("docker");
  });

  it("preserves selected profile scope and bounded public metadata", () => {
    const profile = {
      id: 9,
      label: "selected",
      kind: "container_scope",
      public: { scope_mode: "selected", allowed_containers: "a\nb\nc\nd", allowed_patterns: "api-*" },
      risk_label: "local",
    };
    const target: DockerTarget = { id: 3, connector_kind: "docker", name: "engine", profiles: [profile] };
    expect(formFromTarget({ target })).toMatchObject({ profile_id: "9", scope_mode: "selected", allowed_containers: "a\nb\nc\nd" });
    expect(credentialStateFromRow({ row: { target_id: 3, name: "selected", profile } }).form).toMatchObject({
      target_id: "3",
      scope_mode: "selected",
    });
    expect(credentialRows({ targets: [target] })[0].metadata).toEqual([
      "scope: selected containers",
      "names: a, b, c +1",
      "patterns: api-*",
      "risk: local",
    ]);
  });

  it("updates the exact target profile and credential scope without changing runtime identity", async () => {
    const profile = { id: 9, label: "api", kind: "container_scope" };
    const target = { id: 3, profiles: [profile] };
    const form = { ...emptyForm(), profile_id: "9", transport_target_ref: "ssh:1:2", scope_mode: "selected", allowed_containers: "api" };
    await save({ mode: "edit", form, target });
    expect(api.put).toHaveBeenCalledWith(
      "/api/connector-targets/3/with-profile/9",
      expect.objectContaining({
        profile: expect.objectContaining({
          kind: "container_scope",
          public: { scope_mode: "selected", allowed_containers: "api", allowed_patterns: "" },
        }),
      }),
    );
    await saveCredential({
      operation: "update",
      row: { id: 9, target_id: 3, profile },
      formState: {
        form: {
          target_id: "3",
          profile_label: "api",
          scope_mode: "selected",
          allowed_containers: "api",
          allowed_patterns: "",
          risk_label: "",
        },
      },
    });
    expect(api.put).toHaveBeenLastCalledWith("/api/connector-targets/3/profiles/9", {
      kind: "container_scope",
      label: "api",
      public: { scope_mode: "selected", allowed_containers: "api", allowed_patterns: "" },
      secret: {},
      risk_label: "",
    });
  });
});
