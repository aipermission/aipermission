import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
  delete: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}));

vi.mock("../../../lib/api.js", () => ({
  apiDelete: api.delete,
  apiPost: api.post,
  apiPut: api.put,
}));

import { connectorCredentialRows, createTargetProfileLifecycle } from "./target-profile-lifecycle.js";

function lifecycle(overrides = {}) {
  return createTargetProfileLifecycle({
    connectorKind: "example",
    connectorLabel: "Example",
    targetPayload: (form) => ({ name: form.name, config: { host: form.host } }),
    profilePayload: (form, context) => ({
      kind: context.profile?.kind || "secret",
      label: form.profile_label,
      public: { username: form.username },
      ...(form.password ? { secret: { password: form.password } } : {}),
    }),
    ...overrides,
  });
}

describe("createTargetProfileLifecycle", () => {
  beforeEach(() => vi.clearAllMocks());

  it("creates and updates targets through the atomic target/profile routes", async () => {
    api.post.mockResolvedValueOnce({ profiles: [{ id: 8 }] });
    await lifecycle().save({
      mode: "create",
      form: { name: "example", host: "127.0.0.1", project_id: 2, profile_label: "main", username: "user", password: "secret" },
    });
    expect(api.post).toHaveBeenCalledWith(
      "/api/connector-targets/with-profile",
      expect.objectContaining({ target: expect.objectContaining({ connector_kind: "example", project_id: 2 }) }),
    );

    api.put.mockResolvedValueOnce({ profiles: [{ id: 8 }] });
    await lifecycle().save({
      mode: "edit",
      target: { id: 3, profiles: [{ id: 8, kind: "secret" }] },
      form: { name: "renamed", host: "localhost", project_id: 2, profile_id: "8", profile_label: "main", username: "user" },
    });
    expect(api.put).toHaveBeenCalledWith(
      "/api/connector-targets/3/with-profile/8",
      expect.objectContaining({ target: expect.objectContaining({ name: "renamed", project_id: 2 }) }),
    );
  });

  it("keeps profile CRUD and connection tests on the shared routes", async () => {
    api.post.mockResolvedValueOnce({ id: 9 });
    await expect(
      lifecycle().saveCredential({
        operation: "create",
        formState: { form: { target_id: "3", profile_label: "main", username: "user", password: "secret" } },
      }),
    ).resolves.toEqual({ message: "Example credential created." });
    expect(api.post).toHaveBeenCalledWith("/api/connector-targets/3/profiles", expect.objectContaining({ label: "main" }));

    api.post.mockResolvedValueOnce({ ok: true });
    await expect(lifecycle().test({ target: { id: 3, profiles: [{ id: 9 }] } })).resolves.toMatchObject({ ok: true });
    await lifecycle().deleteCredential({ row: { target_id: 3, id: 9 } });
    await lifecycle().deleteTarget({ target: { id: 3 } });
    expect(api.delete).toHaveBeenNthCalledWith(1, "/api/connector-targets/3/profiles/9");
    expect(api.delete).toHaveBeenNthCalledWith(2, "/api/connector-targets/3");
  });

  it("rejects edits without a loaded profile and unsupported credential operations", async () => {
    await expect(lifecycle().save({ mode: "edit", target: { id: 3, profiles: [] }, form: {} })).rejects.toThrow(
      "Example connector profile is not loaded",
    );
    await expect(lifecycle().saveCredential({ operation: "rotate", formState: { form: {} } })).rejects.toThrow(
      "Unsupported Example credential operation",
    );
  });

  it("maps connector-owned profiles into the shared credential row contract", () => {
    const target = { id: 3, connector_kind: "example", name: "example", profiles: [{ id: 9, label: "main", kind: "secret" }] };
    expect(
      connectorCredentialRows({
        targets: [target, { id: 4, connector_kind: "other", profiles: [{ id: 10 }] }],
        connectorKind: "example",
        connectorLabel: (item) => `Example ${item.id}`,
        targetEndpoint: ({ target: item }) => item.name,
        credentialMetadata: (profile) => [profile.kind],
        includeTarget: true,
      }),
    ).toEqual([
      expect.objectContaining({
        row_id: "example:3:9",
        connector_label: "Example 3",
        target,
        metadata: ["secret"],
        delete_disabled: "",
      }),
    ]);
  });

  it("awaits connector validation before applying a target mutation", async () => {
    let releaseValidation;
    const beforeSave = vi.fn(() => new Promise((resolve) => (releaseValidation = resolve)));
    api.post.mockResolvedValueOnce({ profiles: [{ id: 8 }] });
    const result = lifecycle({ beforeSave }).save({
      mode: "create",
      form: { name: "example", host: "127.0.0.1", profile_label: "main" },
    });

    expect(beforeSave).toHaveBeenCalledOnce();
    expect(api.post).not.toHaveBeenCalled();
    releaseValidation();
    await result;
    expect(api.post).toHaveBeenCalledOnce();
  });
});
