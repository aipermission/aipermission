import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
  delete: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}));

vi.mock("../../../lib/api.ts", () => ({
  apiDelete: api.delete,
  apiPost: api.post,
  apiPut: api.put,
}));

import { connectorCredentialRows, createTargetProfileLifecycle } from "./target-profile-lifecycle.ts";
import type { SetStateAction } from "react";
import type { LifecycleOptions, LifecycleProfile, LifecycleTarget } from "./target-profile-lifecycle-types";

interface ExampleForm {
  name?: string;
  host?: string;
  project_id?: number;
  profile_id?: string;
  profile_label?: string;
  username?: string;
  password?: string;
}
interface ExampleCredentialForm {
  target_id?: string;
  profile_label?: string;
  username?: string;
  password?: string;
}
type ExampleOptions = LifecycleOptions<ExampleForm, ExampleCredentialForm, LifecycleProfile, LifecycleTarget>;

function lifecycle(overrides: Partial<ExampleOptions> = {}) {
  return createTargetProfileLifecycle<ExampleForm, ExampleCredentialForm>({
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
    api.post.mockResolvedValueOnce({ id: 3, profiles: [{ id: 8 }] });
    await lifecycle().save({
      mode: "create",
      form: { name: "example", host: "127.0.0.1", project_id: 2, profile_label: "main", username: "user", password: "secret" },
    });
    expect(api.post).toHaveBeenCalledWith(
      "/api/connector-targets/with-profile",
      expect.objectContaining({ target: expect.objectContaining({ connector_kind: "example", project_id: 2 }) }),
    );

    api.put.mockResolvedValueOnce({ id: 3, profiles: [{ id: 8 }] });
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

  it("updates a credential with connector-owned profile context and feedback", async () => {
    const target = { id: 3, name: "example", profiles: [{ id: 9, kind: "secret" }] };
    const row = { id: 9, target_id: 3, profile: target.profiles[0], target };
    const credentialUpdatedMessage = vi.fn(({ target: selected }: { target: LifecycleTarget | null }) => `Updated ${selected?.name}`);
    const beforeSaveCredential = vi.fn();
    const result = await lifecycle({ credentialUpdatedMessage, beforeSaveCredential }).saveCredential({
      operation: "update",
      row,
      formState: { form: { target_id: "3", profile_label: "reader", username: "read" } },
    });
    expect(api.put).toHaveBeenCalledWith("/api/connector-targets/3/profiles/9", {
      kind: "secret",
      label: "reader",
      public: { username: "read" },
    });
    expect(beforeSaveCredential).toHaveBeenCalledWith(expect.objectContaining({ operation: "update", row, targets: [] }));
    expect(credentialUpdatedMessage).toHaveBeenCalledWith(expect.objectContaining({ row, target }));
    expect(result).toEqual({ message: "Updated example" });
  });

  it("never guesses between multiple profiles or writes after connector validation fails", async () => {
    const target = { id: 3, profiles: [{ id: 9 }, { id: 10 }] };
    await expect(lifecycle().test({ target })).rejects.toThrow("profile is not loaded");
    await expect(lifecycle().save({ mode: "edit", target, form: {} })).rejects.toThrow("profile is not loaded");
    await expect(lifecycle().saveCredential({ operation: "update", formState: { form: {} } })).rejects.toThrow("credential is not loaded");
    const beforeSaveCredential = vi.fn().mockRejectedValue(new Error("scope denied"));
    await expect(
      lifecycle({ beforeSaveCredential }).saveCredential({ operation: "create", formState: { form: { target_id: "3" } } }),
    ).rejects.toThrow("scope denied");
    expect(api.post).not.toHaveBeenCalled();
    expect(api.put).not.toHaveBeenCalled();
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
        targets: [target, { id: 4, name: "other", connector_kind: "other", profiles: [{ id: 10 }] }],
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
    let releaseValidation: () => void = () => {
      throw new Error("validation was not started");
    };
    const beforeSave = vi.fn(() => new Promise<void>((resolve) => (releaseValidation = resolve)));
    api.post.mockResolvedValueOnce({ id: 3, profiles: [{ id: 8 }] });
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

  it("composes functional credential form updates without dropping sibling state", () => {
    let state: { form: { first: boolean | string; second: boolean }; auxiliary: string } = {
      form: { first: true, second: true },
      auxiliary: "preserved",
    };
    const props = lifecycle().credentialFormProps({
      targets: [],
      formState: state,
      setFormState(update: SetStateAction<typeof state>) {
        state = typeof update === "function" ? update(state) : update;
      },
      formMode: "edit",
      state: { state: "idle" },
      onSubmit: vi.fn(),
    });

    props.onChange((current) => ({ ...current, first: false }));
    props.onChange((current) => ({ ...current, second: false }));
    props.onChange({ first: "literal", second: false });

    expect(state).toEqual({ form: { first: "literal", second: false }, auxiliary: "preserved" });
  });
});
