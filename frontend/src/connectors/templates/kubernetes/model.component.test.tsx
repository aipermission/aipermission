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
  targetEndpoint,
} from "./model";
import type { KubernetesTarget } from "./form-types";

const api = vi.hoisted(() => ({ post: vi.fn(), put: vi.fn() }));
vi.mock("../../../lib/api.ts", () => ({ apiPost: api.post, apiPut: api.put, apiDelete: vi.fn() }));

describe("Kubernetes scoped credential model", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.post.mockResolvedValue({ id: 3, profiles: [] });
    api.put.mockResolvedValue({ id: 3, profiles: [] });
  });

  it("projects the supplied runtime identity rather than deriving it from a profile", () => {
    const target = {
      runtime_id: "pod-runtime-51",
      ref: "kubernetes:3:9",
      target_id: 3,
      profile_id: 9,
      name: "cluster",
      profile_label: "namespaces",
      config: { transport_target_ref: "ssh:1:2" },
    };
    expect(liveConsoleRuntimeTarget({ target })).toMatchObject({ id: "pod-runtime-51", target_id: 3, profile_id: 9, target });
  });

  it("uses the shared atomic lifecycle with connector-owned namespace scope", async () => {
    const form = {
      ...emptyForm(),
      transport_target_ref: "ssh:1:2",
      scope_mode: "selected",
      namespaces: "app\nmonitoring",
      context: "dev",
      default_namespace: "app",
    };
    await save({ mode: "create", form });
    expect(api.post).toHaveBeenCalledWith(
      "/api/connector-targets/with-profile",
      expect.objectContaining({
        target: {
          connector_kind: "kubernetes",
          name: "kubernetes",
          project_id: 0,
          config: {
            connection_mode: "over_ssh",
            transport_target_ref: "ssh:1:2",
            kubectl_command: "kubectl",
            context: "dev",
            default_namespace: "app",
          },
        },
        profile: {
          kind: "namespace_scope",
          label: "all-namespaces",
          public: { scope_mode: "selected", namespaces: "app\nmonitoring" },
          secret: {},
          risk_label: "cluster visibility",
        },
      }),
    );
    expect(submitDisabled({ state: { state: "idle" }, form: emptyForm() })).toBe(true);
    expect(submitDisabled({ state: { state: "idle" }, form })).toBe(false);
    expect(syncForm({ form: { ...form, kubectl_command: "" } }).kubectl_command).toBe("kubectl");
  });

  it("retains public namespaces and formats transport context metadata", () => {
    const profile = {
      id: 9,
      label: "selected",
      kind: "namespace_scope",
      public: { scope_mode: "selected", namespaces: "a\nb\nc\nd" },
      risk_label: "read",
    };
    const target: KubernetesTarget = {
      id: 3,
      connector_kind: "kubernetes",
      name: "cluster",
      config: { transport_target_ref: "ssh:1:2", context: "dev", default_namespace: "app" },
      profiles: [profile],
    };
    expect(formFromTarget({ target })).toMatchObject({ profile_id: "9", namespaces: "a\nb\nc\nd" });
    expect(credentialStateFromRow({ row: { target_id: 3, name: "selected", profile } }).form).toMatchObject({
      target_id: "3",
      namespaces: "a\nb\nc\nd",
    });
    expect(targetEndpoint({ target })).toBe("kubectl · ssh:1:2 · context dev · ns app");
    expect(credentialRows({ targets: [target] })[0].metadata).toEqual([
      "scope: selected namespaces",
      "namespaces: a, b, c +1",
      "risk: read",
    ]);
  });

  it("updates only the chosen namespace credential and target profile", async () => {
    const profile = { id: 9, label: "app", kind: "namespace_scope" };
    const target = { id: 3, profiles: [profile] };
    const form = { ...emptyForm(), profile_id: "9", transport_target_ref: "ssh:1:2", scope_mode: "selected", namespaces: "app" };
    await save({ mode: "edit", form, target });
    expect(api.put).toHaveBeenCalledWith(
      "/api/connector-targets/3/with-profile/9",
      expect.objectContaining({
        profile: expect.objectContaining({ kind: "namespace_scope", public: { scope_mode: "selected", namespaces: "app" } }),
      }),
    );
    await saveCredential({
      operation: "update",
      row: { id: 9, target_id: 3, profile },
      formState: { form: { target_id: "3", profile_label: "app", scope_mode: "selected", namespaces: "app", risk_label: "" } },
    });
    expect(api.put).toHaveBeenLastCalledWith("/api/connector-targets/3/profiles/9", {
      kind: "namespace_scope",
      label: "app",
      public: { scope_mode: "selected", namespaces: "app" },
      secret: {},
      risk_label: "",
    });
  });
});
