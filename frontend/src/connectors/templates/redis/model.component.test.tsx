import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  credentialRows,
  credentialStateFromRow,
  emptyCredentialState,
  emptyForm,
  formFromTarget,
  save,
  saveCredential,
  serverProductLabel,
  submitLabel,
  syncForm,
  targetEndpoint,
} from "./model";
import type { RedisTarget } from "./form-types";

const api = vi.hoisted(() => ({ post: vi.fn(), put: vi.fn() }));
vi.mock("../../../lib/api.js", () => ({ apiPost: api.post, apiPut: api.put, apiDelete: vi.fn() }));

describe("Redis connector model", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.post.mockResolvedValue({ profiles: [] });
    api.put.mockResolvedValue({ profiles: [] });
  });

  it("normalizes target payloads while preserving the encrypted password on edit", async () => {
    const profile = { id: 9, label: "read", kind: "username_password" };
    const target = { id: 3, profiles: [profile] };
    const form = {
      ...emptyForm(),
      name: "cache",
      profile_id: "9",
      server_family: "valkey",
      connection_mode: "over_ssh",
      transport_target_ref: "ssh:1:2",
      port: "6380",
      database: "2",
      tls_mode: "verify_full",
      profile_label: "read",
      username: "reader",
      password: "",
    };
    await save({ mode: "edit", target, form });
    expect(api.put).toHaveBeenCalledWith("/api/connector-targets/3/with-profile/9", {
      target: {
        name: "cache",
        project_id: 0,
        config: {
          server_family: "valkey",
          connection_mode: "over_ssh",
          host: "127.0.0.1",
          port: 6380,
          database: 2,
          tls_mode: "verify_full",
          transport_target_ref: "ssh:1:2",
        },
      },
      profile: { kind: "username_password", label: "read", public: { username: "reader" }, risk_label: "cache access" },
    });
    await saveCredential({
      operation: "update",
      row: { id: 9, target_id: 3, profile },
      formState: { form: { target_id: "3", profile_label: "read", username: "reader", password: "", risk_label: "read" } },
    });
    expect(api.put).toHaveBeenLastCalledWith("/api/connector-targets/3/profiles/9", {
      kind: "username_password",
      label: "read",
      public: { username: "reader" },
      risk_label: "read",
    });
  });

  it("injects new passwords only into the secret payload and reports the selected product", async () => {
    const target = { id: 3, config: { server_family: "valkey" } };
    const result = await saveCredential({
      operation: "create",
      targets: [target],
      formState: {
        form: { target_id: "3", profile_label: "service", username: "service", password: "test-password", risk_label: "write" },
      },
    });
    expect(api.post).toHaveBeenCalledWith("/api/connector-targets/3/profiles", {
      kind: "username_password",
      label: "service",
      public: { username: "service" },
      secret: { password: "test-password" },
      risk_label: "write",
    });
    expect(result.message).toBe("Valkey credential created.");
  });
  it("keeps direct and SSH-backed forms on the same normalized contract", () => {
    const direct = emptyForm();
    expect(syncForm({ form: { ...direct, transport_target_ref: "ssh:1:1" } })).toMatchObject({
      connection_mode: "direct",
      transport_target_ref: "",
    });

    const tunneled = syncForm({
      form: { ...direct, connection_mode: "over_ssh", transport_target_ref: "ssh:1:1" },
    });
    expect(tunneled.transport_target_ref).toBe("ssh:1:1");
    expect(targetEndpoint({ target: { config: { ...tunneled, port: Number(tunneled.port), database: Number(tunneled.database) } } })).toBe(
      "127.0.0.1:6379/0 · over ssh",
    );
    expect(serverProductLabel({ server_family: "valkey" })).toBe("Valkey");
  });

  it("reports stable submit labels for connector editor state", () => {
    expect(submitLabel({ state: { state: "saving" }, mode: "create" })).toBe("Saving...");
    expect(submitLabel({ state: { state: "ready" }, mode: "edit" })).toBe("Save changes");
    expect(submitLabel({ state: { state: "ready" }, mode: "create" })).toBe("Create connector");
  });

  it("keeps product labels and public credential rows separate from encrypted values", () => {
    const target: RedisTarget = {
      id: 7,
      connector_kind: "redis",
      name: "cache",
      config: { server_family: "valkey", database: 2 },
      profiles: [{ id: 9, label: "reader", kind: "username_password", public: { username: "read" }, risk_label: "readonly" }],
    };
    expect(formFromTarget({ target })).toMatchObject({ profile_id: "9", server_family: "valkey", database: 2, password: "" });
    expect(credentialStateFromRow({ row: { target_id: 7, name: "reader", profile: target.profiles?.[0] } })).toEqual({
      form: { target_id: "7", profile_label: "reader", username: "read", password: "", risk_label: "readonly" },
    });
    expect(emptyCredentialState({ targets: [target] }).form.target_id).toBe("7");
    expect(credentialRows({ targets: [target] })).toEqual([
      expect.objectContaining({
        connector_label: "Valkey",
        target_detail: "127.0.0.1:6379/2 · direct",
        metadata: ["username: read", "risk: readonly"],
      }),
    ]);
  });
});
