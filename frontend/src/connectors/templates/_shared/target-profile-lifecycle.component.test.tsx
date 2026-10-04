import { beforeEach, expect, it, vi } from "vitest";
import type { SetStateAction } from "react";
import { connectorCredentialRows, createTargetProfileLifecycle } from "./target-profile-lifecycle";

const api = vi.hoisted(() => ({ post: vi.fn(), put: vi.fn(), delete: vi.fn() }));
vi.mock("../../../lib/api.ts", () => ({ apiDelete: api.delete, apiPost: api.post, apiPut: api.put }));

function lifecycle() {
  return createTargetProfileLifecycle<{ name?: string; project_id?: number }, { target_id?: string; profile_label?: string }>({
    connectorKind: "example",
    connectorLabel: "Example",
    targetPayload: (form) => ({ name: form.name }),
    profilePayload: (form) => ({ label: "profile_label" in form ? form.profile_label : "main" }),
  });
}

beforeEach(() => vi.clearAllMocks());

it("wires generic persistence into the standard template constructor", async () => {
  api.post.mockResolvedValue({ id: 3, profiles: [{ id: 8 }] });
  const model = lifecycle();
  await model.save({ mode: "create", form: { name: "Example", project_id: 2 } });
  expect(api.post).toHaveBeenLastCalledWith("/api/connector-targets/with-profile", {
    target: { connector_kind: "example", name: "Example", project_id: 2 },
    profile: { label: "main" },
  });
  await expect(
    model.saveCredential({ operation: "create", formState: { form: { target_id: "3", profile_label: "reader" } } }),
  ).resolves.toEqual({
    message: "Example credential created.",
  });
  expect(api.post).toHaveBeenLastCalledWith("/api/connector-targets/3/profiles", { label: "reader" });
});

it("keeps connection testing separate from persistence and refuses ambiguous profiles", async () => {
  const model = lifecycle();
  await expect(model.test({ target: { id: 3, profiles: [{ id: 9 }, { id: 10 }] } })).rejects.toThrow("profile is not loaded");
  expect(api.post).not.toHaveBeenCalled();
  api.post.mockResolvedValue({ ok: true });
  await expect(model.test({ target: { id: 3, profiles: [{ id: 9 }] } })).resolves.toMatchObject({ ok: true });
  expect(api.post).toHaveBeenLastCalledWith("/api/connector-targets/3/profiles/9/test", {});
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
    expect.objectContaining({ row_id: "example:3:9", connector_label: "Example 3", target, metadata: ["secret"], delete_disabled: "" }),
  ]);
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
