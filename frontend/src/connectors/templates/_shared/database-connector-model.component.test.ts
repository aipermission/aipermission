import { beforeEach, expect, expectTypeOf, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { createDatabaseConnectorModel } from "./database-connector-model";
import type { DatabaseModelForm, DatabaseTarget, DatabaseTargetDefaults } from "./database-model-types";

vi.mock("../../../lib/api", () => ({ apiDelete: vi.fn(), apiPost: vi.fn(), apiPut: vi.fn() }));

const targetDefaults: DatabaseTargetDefaults = {
  name: "Database",
  connection_mode: "direct",
  host: "127.0.0.1",
  port: 5432,
  database: "app",
  transport_target_ref: "",
};
const credentialDefaults = { target_id: "", profile_label: "readonly", username: "", password: "", risk_label: "read-only" };
const target: DatabaseTarget = {
  id: 4,
  name: "Database",
  connector_kind: "database",
  profiles: [{ id: 8, label: "reader", kind: "username_password", public: { username: "reader" } }],
};
function model(includeEmptyPassword = false) {
  return createDatabaseConnectorModel({
    kind: "database",
    label: "Database",
    defaultRiskLabel: "read-only",
    targetDefaults,
    credentialDefaults,
    includeEmptyPassword,
    targetForm: () => targetDefaults,
    targetConfig: (form) => ({ host: form.host, port: Number(form.port) }),
    targetEndpoint: ({ target }) => target.name,
  });
}

beforeEach(() => {
  vi.mocked(apiPost)
    .mockReset()
    .mockResolvedValue({ ...target, ok: true, message: "Connected" });
  vi.mocked(apiPut).mockReset().mockResolvedValue(target);
  vi.mocked(apiDelete).mockReset().mockResolvedValue(undefined);
});

it("describes local connector deletion without implying external database deletion", () => {
  const connector = model();
  expect(connector.deleteDialog({ target })).toEqual({
    title: "Delete Database",
    description: "Remove this Database connector target, credential profiles, and token action permissions from aipermission.",
    details: [
      { label: "Connector", value: "Database" },
      { label: "Reference", value: "database:4" },
    ],
    notice: "This removes the connector target and its credential profiles. It does not change the external Database service.",
    actions: [
      { label: "Cancel", action: "close", variant: "outline" },
      { label: "Delete connector", pendingLabel: "Deleting...", removeKey: false },
    ],
  });
  expect(connector.deleteDialog({})).toMatchObject({
    title: "Delete connector",
    details: [
      { label: "Connector", value: undefined },
      { label: "Reference", value: "" },
    ],
  });
});

it("preserves encrypted credentials on metadata-only edits and sends secrets only for rotation", async () => {
  const connector = model();
  const form = { ...credentialDefaults, target_id: "4", profile_label: "reader", username: "reader" };
  const row = { id: 8, target_id: 4, name: "reader", profile: target.profiles?.[0] };

  await connector.saveCredential({ operation: "update", row, formState: { form } });
  expect(apiPut).toHaveBeenLastCalledWith("/api/connector-targets/4/profiles/8", {
    kind: "username_password",
    label: "reader",
    public: { username: "reader" },
    risk_label: "read-only",
  });
  await connector.saveCredential({ operation: "update", row, formState: { form: { ...form, password: "test-only-password" } } });
  expect(apiPut).toHaveBeenLastCalledWith(
    "/api/connector-targets/4/profiles/8",
    expect.objectContaining({ secret: { password: "test-only-password" } }),
  );
  expect(connector.credentialRows({ targets: [target] })[0]).not.toHaveProperty("password");
});

it("keeps empty-password creation a connector-specific choice", async () => {
  const form = { ...credentialDefaults, target_id: "4" };
  await model(false).saveCredential({ operation: "create", formState: { form } });
  expect(apiPost).toHaveBeenLastCalledWith("/api/connector-targets/4/profiles", expect.objectContaining({ secret: {} }));
  await model(true).saveCredential({ operation: "create", formState: { form } });
  expect(apiPost).toHaveBeenLastCalledWith("/api/connector-targets/4/profiles", expect.objectContaining({ secret: { password: "" } }));
});

it("rejects ambiguous profiles before making connection-test or update calls", async () => {
  const connector = model();
  const ambiguousTarget = { ...target, profiles: [...(target.profiles ?? []), { id: 9, label: "admin", kind: "username_password" }] };
  await expect(connector.test({ target: ambiguousTarget })).rejects.toThrow("Connector profile is not loaded.");
  const form: DatabaseModelForm<DatabaseTargetDefaults> = { ...connector.emptyForm(), profile_id: "missing" };
  await expect(connector.save({ mode: "edit", target: ambiguousTarget, form })).rejects.toThrow("connector profile is not loaded");
  expect(apiPost).not.toHaveBeenCalled();
  expect(apiPut).not.toHaveBeenCalled();
});

it("uses the selected profile for tests and atomically saves a target with its initial profile", async () => {
  const connector = model();
  const response = await connector.test({ target });
  expect(response).toMatchObject({ ok: true, data: { message: "Connected" } });
  expect(apiPost).toHaveBeenCalledWith("/api/connector-targets/4/profiles/8/test", {});
  await connector.save({ mode: "create", form: { ...connector.emptyForm(), project_id: "3" } });
  expect(apiPost).toHaveBeenLastCalledWith(
    "/api/connector-targets/with-profile",
    expect.objectContaining({
      target: { connector_kind: "database", name: "Database", config: { host: "127.0.0.1", port: 5432 }, project_id: 3 },
      profile: expect.objectContaining({ secret: {}, label: "readonly" }),
    }),
  );
});

it("keeps deletion scoped to the selected target and profile", async () => {
  const connector = model();
  await connector.deleteCredential({ row: { id: 8, target_id: 4 } });
  expect(apiDelete).toHaveBeenLastCalledWith("/api/connector-targets/4/profiles/8");
  await connector.deleteTarget({ target });
  expect(apiDelete).toHaveBeenLastCalledWith("/api/connector-targets/4");
});

it("preserves native profile types and independent runtime IDs in display rows", () => {
  const nativeTarget = {
    ...target,
    profiles: [{ id: 77, label: "Selected", kind: "identity", runtime_id: 91, public: { username: "reader", managed: true } }],
  };
  const rows = model().credentialRows<(typeof nativeTarget.profiles)[number], typeof nativeTarget>({ targets: [nativeTarget] });
  expectTypeOf(rows[0].profile.runtime_id).toEqualTypeOf<number>();
  expectTypeOf(rows[0].profile.public.managed).toEqualTypeOf<boolean>();
  expect(rows[0]).toMatchObject({ id: 77, target_id: 4, profile: { runtime_id: 91 } });
  expect(rows[0].profile).toBe(nativeTarget.profiles[0]);
});

it("widens normalized transport fields without erasing unrelated form types", () => {
  const connector = model();
  const direct = connector.syncForm({
    form: { connector_kind: "database", connection_mode: "direct", transport_target_ref: "ssh:1:1" as const, port: 5432 as const },
  });
  expectTypeOf(direct.transport_target_ref).toEqualTypeOf<string | undefined>();
  expectTypeOf(direct.port).toEqualTypeOf<5432>();
  expect(direct.transport_target_ref).toBe("");
  const tunneled = connector.syncForm({ form: { connector_kind: "database", connection_mode: "over_ssh", host: "" as const } });
  expectTypeOf(tunneled.host).toEqualTypeOf<string | undefined>();
  expect(tunneled.host).toBe("127.0.0.1");
  const complete = connector.syncEditorForm({ form: { ...connector.emptyForm(), connection_mode: "over_ssh", host: "" } });
  expectTypeOf(complete).toEqualTypeOf<ReturnType<typeof connector.emptyForm>>();
  expectTypeOf(complete.host).toEqualTypeOf<string>();
  expectTypeOf(complete.transport_target_ref).toEqualTypeOf<string>();
  type LiteralDefaults = DatabaseTargetDefaults & { host: ""; transport_target_ref: "" };
  expectTypeOf<DatabaseModelForm<LiteralDefaults>["host"]>().toEqualTypeOf<string>();
  expectTypeOf<DatabaseModelForm<LiteralDefaults>["transport_target_ref"]>().toEqualTypeOf<string>();
  expect(complete.host).toBe("127.0.0.1");
});

it("types connector-specific credential and target serializers independently", async () => {
  const connector = createDatabaseConnectorModel({
    kind: "database",
    label: "Database",
    defaultRiskLabel: "read-only",
    targetDefaults: { ...targetDefaults, organization: "target-organization" },
    credentialDefaults: { ...credentialDefaults, tenant_id: "default-tenant" },
    targetForm: () => ({ ...targetDefaults, organization: "target-organization" }),
    targetConfig: (form) => ({ host: form.host }),
    targetEndpoint: ({ target }) => target.name,
    credentialPublic: (form) => ({ username: form.username, tenant_id: form.tenant_id }),
    targetCredentialPublic: (form) => ({ username: form.username, organization: form.organization }),
  });

  await connector.saveCredential({
    operation: "create",
    formState: { form: { ...credentialDefaults, target_id: "4", tenant_id: "chosen-tenant" } },
  });
  expect(apiPost).toHaveBeenLastCalledWith(
    "/api/connector-targets/4/profiles",
    expect.objectContaining({ public: { username: "", tenant_id: "chosen-tenant" } }),
  );
  await connector.save({ mode: "create", form: connector.emptyForm() });
  expect(apiPost).toHaveBeenLastCalledWith(
    "/api/connector-targets/with-profile",
    expect.objectContaining({ profile: expect.objectContaining({ public: { username: "", organization: "target-organization" } }) }),
  );
});
