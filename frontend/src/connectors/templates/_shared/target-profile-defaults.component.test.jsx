import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({ post: vi.fn(), put: vi.fn(), delete: vi.fn() }));
vi.mock("../../../lib/api.js", () => ({ apiPost: api.post, apiPut: api.put, apiDelete: api.delete }));

import * as docker from "../docker/model.js";
import * as kafka from "../kafka/model.js";
import * as kubernetes from "../kubernetes/model.js";
import * as mail from "../mail/model.js";
import * as rabbitmq from "../rabbitmq/model.js";
import * as s3 from "../s3/model.js";
import * as redis from "../redis/model.js";
import {
  defaultTargetProfile,
  firstTargetCredentialForm,
  standardSubmitLabel,
  usernameCredentialStateFromRow,
  usesStandardTargetProfileLifecycle,
} from "./target-profile-lifecycle.js";

const models = [
  ["docker", docker, "all-containers", "container access", "container_scope"],
  ["kafka", kafka, "monitor", "stream read", "sasl"],
  ["kubernetes", kubernetes, "all-namespaces", "cluster visibility", "namespace_scope"],
  ["mail", mail, "mailbox", "mailbox access", "password"],
  ["rabbitmq", rabbitmq, "monitor", "queue access", "username_password"],
  ["s3", s3, "default", "object storage", "access_key"],
  ["redis", redis, "default", "cache access", "username_password"],
];
const idle = { state: "idle" };
const saving = { state: "saving" };
const publicDefaults = {
  docker: { scope_mode: "all", allowed_containers: "", allowed_patterns: "" },
  kafka: { mechanism: "none", username: "" },
  kubernetes: { scope_mode: "all", namespaces: "" },
  mail: {
    mailbox_address: "",
    display_name: "",
    reply_to: "",
    imap_enabled: true,
    smtp_auth_mode: "disabled",
    allowed_read_folders: ["INBOX"],
    allowed_mutation_source_folders: ["INBOX"],
    allowed_mutation_destination_folders: [],
    sent_folder: "",
    archive_folder: "",
    trash_folder: "",
  },
  rabbitmq: { username: "" },
  s3: { access_key_id: "" },
  redis: { username: "" },
};
const configDefaults = {
  docker: { connection_mode: "over_ssh", transport_target_ref: "", docker_command: "docker" },
  kafka: {
    server_family: "kafka",
    connection_mode: "direct",
    bootstrap_brokers: "127.0.0.1:9092",
    transport_target_ref: "",
    tls_enabled: false,
    allow_insecure_plain_sasl: false,
    tls_server_name: "",
    tls_ca_pem: "",
  },
  kubernetes: {
    connection_mode: "over_ssh",
    transport_target_ref: "",
    kubectl_command: "kubectl",
    context: "",
    default_namespace: "",
  },
  mail: {
    connection_mode: "direct",
    transport_target_ref: "",
    imap_host: "imap.example.com",
    imap_port: 993,
    imap_tls_mode: "implicit_tls",
    smtp_host: "smtp.example.com",
    smtp_port: 465,
    smtp_tls_mode: "implicit_tls",
    allowed_recipient_domains: [],
  },
  rabbitmq: { connection_mode: "direct", scheme: "auto", host: "127.0.0.1", port: 15672, vhost: "/", transport_target_ref: "" },
  s3: {
    connection_mode: "direct",
    scheme: "https",
    host: "s3.amazonaws.com",
    port: 443,
    region: "us-east-1",
    bucket: "",
    path_style: true,
    trust_conditional_requests: false,
    transport_target_ref: "",
  },
  redis: {
    server_family: "redis",
    connection_mode: "direct",
    host: "127.0.0.1",
    port: 6379,
    database: 0,
    tls_mode: "auto",
    transport_target_ref: "",
  },
};

beforeEach(() => {
  vi.resetAllMocks();
  api.post.mockResolvedValue({ profiles: [{ id: 9 }] });
  api.put.mockResolvedValue({ profiles: [{ id: 9 }] });
});

describe("shared target/profile form defaults", () => {
  it("selects only the first matching target and returns an independent form", () => {
    const defaults = Object.freeze({ target_id: "stale", profile_label: "monitor" });
    const targets = [
      { id: 1, connector_kind: "other" },
      { id: 2, connector_kind: "redis" },
      { id: 3, connector_kind: "redis" },
    ];
    const result = firstTargetCredentialForm(targets, "redis", defaults);
    expect(result).toEqual({ form: { target_id: "2", profile_label: "monitor" } });
    result.form.profile_label = "changed";
    expect(defaults.profile_label).toBe("monitor");
    expect(firstTargetCredentialForm(targets, "missing", defaults).form.target_id).toBe("");
    expect(firstTargetCredentialForm([{ connector_kind: "redis" }], "redis", defaults).form.target_id).toBe("");
  });

  it.each([
    [saving, "edit", "Saving..."],
    [saving, "create", "Saving..."],
    [idle, "edit", "Save changes"],
    [idle, "create", "Create connector"],
  ])("labels %j in %s mode as %s", (state, mode, label) => {
    expect(standardSubmitLabel({ state, mode })).toBe(label);
  });

  it("maps public username fields without exposing stored passwords", () => {
    const row = {
      target_id: 42,
      name: "operator",
      profile: { public: { username: "alice" }, secret: { password: "hidden" }, risk_label: "read" },
    };
    expect(usernameCredentialStateFromRow(row)).toEqual({
      form: { target_id: "42", profile_label: "operator", username: "alice", password: "", risk_label: "read" },
    });
    for (const profile of [undefined, {}, { public: {} }]) {
      expect(usernameCredentialStateFromRow({ name: "blank", profile })).toEqual({
        form: { target_id: "", profile_label: "blank", username: "", password: "", risk_label: "" },
      });
    }
  });

  it("uses an explicit profile, a sole profile, or the supplied fallback", () => {
    const profile = { id: 9 };
    const fallback = { label: "fallback" };
    expect(defaultTargetProfile({ profiles: [{ id: 8 }] }, profile)).toBe(profile);
    expect(defaultTargetProfile({ profiles: [profile] })).toBe(profile);
    expect(defaultTargetProfile({ profiles: [profile, { id: 8 }] }, null, fallback)).toBe(fallback);
    expect(defaultTargetProfile()).toEqual({});
    expect(usesStandardTargetProfileLifecycle(null)).toBe(false);
    expect(usesStandardTargetProfileLifecycle({ ...redis, save: async () => {} })).toBe(false);
  });
});

describe.each(models)("%s real lifecycle form defaults", (kind, model, label, risk, profileKind) => {
  it("creates fresh connector and credential forms and selects the first matching target", () => {
    expect(usesStandardTargetProfileLifecycle(model)).toBe(true);
    const form = model.emptyForm();
    expect(form).toMatchObject({ connector_kind: kind, profile_label: label, risk_label: risk });
    form.profile_label = "changed";
    expect(model.emptyForm().profile_label).toBe(label);
    const empty = model.emptyCredentialState();
    expect(empty.form).toMatchObject({ target_id: "", profile_label: label, risk_label: risk });
    expect(model.emptyCredentialState({}).form).toEqual(empty.form);
    const targets = [
      { id: 1, connector_kind: "unrelated" },
      { id: 42, connector_kind: kind },
      { id: 43, connector_kind: kind },
    ];
    expect(model.emptyCredentialState({ targets }).form).toEqual({ ...empty.form, target_id: "42" });
    empty.form.profile_label = "changed";
    expect(model.emptyCredentialState().form.profile_label).toBe(label);
  });

  it("hydrates a sole or explicitly selected edit profile without leaking secrets", () => {
    const profile = { id: 9, label: "chosen", public: publicDefaults[kind], secret: { password: "hidden" }, risk_label: "custom" };
    const target = { name: "configured", config: configDefaults[kind], profiles: [profile] };
    const edit = model.formFromTarget({ target });
    expect(edit).toMatchObject({
      connector_kind: kind,
      profile_id: "9",
      name: "configured",
      profile_label: "chosen",
      risk_label: "custom",
    });
    expect(model.formFromTarget({ target: { ...target, profiles: [profile, { id: 10 }] }, profile })).toEqual(edit);
    for (const profiles of [undefined, [], [profile, { id: 10 }]]) {
      expect(model.formFromTarget({ target: { profiles } })).toMatchObject({
        profile_id: "",
        name: "",
        profile_label: label,
        risk_label: risk,
      });
    }
    const row = { target_id: 42, name: profile.label, profile };
    expect(model.credentialStateFromRow({ row }).form).toMatchObject({ target_id: "42", profile_label: "chosen", risk_label: "custom" });
    expect(model.credentialStateFromRow({ row: { name: label } }).form).toMatchObject({ target_id: "", profile_label: label });
    for (const secretField of ["password", "secret_access_key", "session_token", "imap_password", "smtp_password"]) {
      if (secretField in edit) expect(edit[secretField]).toBe("");
    }
  });

  it("normalizes its own form, leaves other connectors alone, and reflects save state", () => {
    const other = { connector_kind: "other" };
    expect(model.syncForm({ form: other })).toBe(other);
    const form = { ...model.emptyForm(), transport_target_ref: "ssh:1:2" };
    const normalized = model.syncForm({ form });
    expect(normalized).not.toBe(form);
    expect(normalized.transport_target_ref).toBe(normalized.connection_mode === "direct" ? "" : "ssh:1:2");
    expect(form.transport_target_ref).toBe("ssh:1:2");
    expect(model.submitDisabled({ state: idle, form })).toBe(false);
    expect(model.submitDisabled({ state: saving, form })).toBe(true);
    expect(model.submitLabel({ state: idle, mode: "create" })).toBe("Create connector");
    expect(model.submitLabel({ state: idle, mode: "edit" })).toBe("Save changes");
    expect(model.submitLabel({ state: saving, mode: "edit" })).toBe("Saving...");
  });

  it.each(["create", "edit"])("preserves sibling state and routes %s credential form submissions", (formMode) => {
    const targets = [{ id: 42, connector_kind: kind }];
    let current = { ...model.emptyCredentialState({ targets }), auxiliary: "keep" };
    const onSubmit = vi.fn();
    const props = model.credentialFormProps({
      targets,
      formState: current,
      formMode,
      state: idle,
      onSubmit,
      setFormState: (update) => {
        current = update(current);
      },
    });
    expect(props).toMatchObject({ form: current.form, formMode, targets, state: idle });
    props.onChange((form) => ({ ...form, profile_label: "renamed" }));
    props.onChange((form) => ({ ...form, risk_label: "updated" }));
    expect(current).toMatchObject({ auxiliary: "keep", form: { profile_label: "renamed", risk_label: "updated", target_id: "42" } });
    props.onChange({ target_id: "43", profile_label: "replacement" });
    expect(current).toEqual({ auxiliary: "keep", form: { target_id: "43", profile_label: "replacement" } });
    const event = { preventDefault: vi.fn() };
    props.onSubmit(event);
    expect(onSubmit).toHaveBeenCalledExactlyOnceWith(event, formMode === "edit" ? "update" : "create");
    expect(api.post).not.toHaveBeenCalled();
  });

  it("sends real default target/profile payloads and preserves edit profile identity", async () => {
    const form = { ...model.emptyForm(), project_id: "7" };
    await model.save({ mode: "create", form });
    expect(api.post).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/with-profile", {
      target: { connector_kind: kind, name: form.name, project_id: 7, config: configDefaults[kind] },
      profile: expect.objectContaining({ kind: profileKind, label, public: publicDefaults[kind], risk_label: risk }),
    });
    const target = { id: 42, profiles: [{ id: 8 }, { id: 9, kind: profileKind }] };
    await model.save({ mode: "edit", target, form: { ...form, profile_id: "9", name: "renamed" } });
    expect(api.put).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/42/with-profile/9", {
      target: { name: "renamed", project_id: 7, config: configDefaults[kind] },
      profile: expect.objectContaining({ kind: profileKind, label, public: publicDefaults[kind], risk_label: risk }),
    });
    if (["mail", "s3", "rabbitmq", "redis"].includes(kind)) expect(api.put.mock.calls[0][1].profile).not.toHaveProperty("secret");
    await expect(model.save({ mode: "edit", target, form: { ...form, profile_id: "missing" } })).rejects.toThrow(
      "connector profile is not loaded",
    );
  });

  it("uses real credential defaults for create/update and rejects missing or unsupported edits", async () => {
    const formState = model.emptyCredentialState({ targets: [{ id: 42, connector_kind: kind }] });
    const created = await model.saveCredential({ operation: "create", formState });
    expect(created.message).toContain("created.");
    expect(api.post).toHaveBeenCalledExactlyOnceWith(
      "/api/connector-targets/42/profiles",
      expect.objectContaining({ kind: profileKind, label, public: publicDefaults[kind] }),
    );
    const row = { id: 9, target_id: 42, profile: { kind: profileKind } };
    const updated = await model.saveCredential({ operation: "update", row, formState });
    expect(updated.message).toContain("updated.");
    expect(api.put).toHaveBeenCalledExactlyOnceWith(
      "/api/connector-targets/42/profiles/9",
      expect.objectContaining({ kind: profileKind, label, public: publicDefaults[kind] }),
    );
    await expect(model.saveCredential({ operation: "update", formState })).rejects.toThrow("not loaded");
    await expect(model.saveCredential({ operation: "rotate", formState })).rejects.toThrow("Unsupported");
  });

  it("tests the sole or explicit profile and deletes only the selected resources", async () => {
    const profile = { id: 9 };
    const target = { id: 42, profiles: [profile] };
    api.post.mockResolvedValueOnce({ ok: true });
    await expect(model.test({ target })).resolves.toEqual({ ok: true, error: null, data: { ok: true } });
    expect(api.post).toHaveBeenLastCalledWith("/api/connector-targets/42/profiles/9/test", {});
    api.post.mockResolvedValueOnce({ ok: false, message: "denied" });
    await expect(model.test({ target, profile: { id: 10 } })).resolves.toMatchObject({ ok: false, error: "denied" });
    expect(api.post).toHaveBeenLastCalledWith("/api/connector-targets/42/profiles/10/test", {});
    await expect(model.test({ target: { id: 42, profiles: [] } })).rejects.toThrow("connector profile is not loaded");
    await model.deleteCredential({ row: { target_id: 42, id: 9 } });
    await model.deleteTarget({ target });
    expect(api.delete.mock.calls).toEqual([["/api/connector-targets/42/profiles/9"], ["/api/connector-targets/42"]]);
  });
});

describe("connector-specific form branches", () => {
  it("retains public username data in RabbitMQ and Redis rows but clears passwords", () => {
    const row = { target_id: 42, name: "operator", profile: { public: { username: "alice" }, risk_label: "read" } };
    for (const model of [rabbitmq, redis]) expect(model.credentialStateFromRow({ row })).toEqual(usernameCredentialStateFromRow(row));
  });

  it("normalizes transport-only scope commands and disables targets without transport", () => {
    for (const [model, command, fallback] of [
      [docker, "docker_command", "docker"],
      [kubernetes, "kubectl_command", "kubectl"],
    ]) {
      const form = { ...model.emptyForm(), connection_mode: "direct", [command]: "" };
      expect(model.syncForm({ form })).toMatchObject({ connection_mode: "over_ssh", [command]: fallback });
      expect(model.submitDisabled({ state: idle, form })).toBe(true);
    }
  });

  it("resets disabled Kafka TLS/SASL fields and retains enabled credentials", () => {
    const form = { ...kafka.emptyForm(), tls_server_name: "broker", tls_ca_pem: "pem", username: "alice", password: "secret" };
    expect(kafka.syncForm({ form })).toMatchObject({ tls_server_name: "", tls_ca_pem: "", username: "", password: "" });
    const enabled = { ...form, tls_enabled: true, sasl_mechanism: "plain", connection_mode: "over_ssh", transport_target_ref: "ssh:1:2" };
    expect(kafka.syncForm({ form: enabled })).toEqual(enabled);
    const row = { target_id: 42, name: "sasl", profile: { public: { mechanism: "plain", username: "alice" } } };
    expect(kafka.credentialStateFromRow({ row }).form).toMatchObject({
      sasl_mechanism: "plain",
      existing_sasl_mechanism: "plain",
      username: "alice",
      password: "",
    });
  });

  it("validates Mail protocols before target and credential API mutations", async () => {
    const form = { ...mail.emptyForm(), imap_enabled: false, smtp_auth_mode: "disabled" };
    expect(mail.submitDisabled({ state: idle, form })).toBe(true);
    await expect(mail.save({ mode: "create", form })).rejects.toThrow("Enable IMAP or SMTP before saving this Mail connector.");
    await expect(mail.saveCredential({ operation: "create", formState: { form } })).rejects.toThrow(
      "Enable IMAP or SMTP before saving this Mail credential.",
    );
    expect(api.post).not.toHaveBeenCalled();
    expect(mail.syncForm({ form: { ...form, smtp_auth_mode: "reuse_imap", imap_tls_mode: "", smtp_tls_mode: "" } })).toMatchObject({
      smtp_auth_mode: "disabled",
      imap_tls_mode: "implicit_tls",
      smtp_tls_mode: "implicit_tls",
    });
    expect(mail.submitDisabled({ state: idle, form: { ...form, smtp_auth_mode: "separate" } })).toBe(false);
  });

  it("restores S3/RabbitMQ missing defaults without discarding SSH transport", () => {
    expect(s3.syncForm({ form: { ...s3.emptyForm(), scheme: "", port: 0, region: "" } })).toMatchObject({
      scheme: "https",
      port: 443,
      region: "us-east-1",
    });
    expect(s3.syncForm({ form: { ...s3.emptyForm(), scheme: "http", port: 0 } }).port).toBe(80);
    for (const model of [s3, rabbitmq, redis]) {
      const form = { ...model.emptyForm(), connection_mode: "over_ssh", transport_target_ref: "ssh:1:2" };
      expect(model.syncForm({ form }).transport_target_ref).toBe("ssh:1:2");
    }
    expect(rabbitmq.syncForm({ form: { ...rabbitmq.emptyForm(), scheme: "", vhost: "" } })).toMatchObject({ scheme: "http", vhost: "/" });
  });

  it("uses Valkey credential messages from a matching target or the loaded row", async () => {
    const target = { id: 42, config: { server_family: "valkey" } };
    const formState = redis.emptyCredentialState({ targets: [{ ...target, connector_kind: "redis" }] });
    await expect(redis.saveCredential({ operation: "create", formState, targets: [target] })).resolves.toEqual({
      message: "Valkey credential created.",
    });
    await expect(redis.saveCredential({ operation: "update", formState, row: { id: 9, target, profile: {} } })).resolves.toEqual({
      message: "Valkey credential updated.",
    });
  });
});
