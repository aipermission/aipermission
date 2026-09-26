import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  credentialRows,
  credentialStateFromRow,
  emptyCredentialState,
  emptyForm,
  formFromTarget,
  save,
  saveCredential,
  syncForm,
  targetEndpoint,
} from "./model";
import type { KafkaTarget } from "./form-types";

const api = vi.hoisted(() => ({ post: vi.fn(), put: vi.fn() }));
vi.mock("../../../lib/api.ts", () => ({ apiPost: api.post, apiPut: api.put, apiDelete: vi.fn() }));

describe("Kafka credential lifecycle model", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.post.mockResolvedValue({ id: 3, profiles: [] });
    api.put.mockResolvedValue({ id: 3, profiles: [] });
  });

  it("normalizes transport, TLS, and SASL clearing before reuse", () => {
    const form = {
      ...emptyForm(),
      transport_target_ref: "ssh:1:2",
      tls_server_name: "old-name",
      tls_ca_pem: "old-ca",
      username: "old-user",
      password: "old-password",
    };
    expect(syncForm({ form })).toMatchObject({ transport_target_ref: "", tls_server_name: "", tls_ca_pem: "", username: "", password: "" });
    const secured = { ...form, connection_mode: "over_ssh", tls_enabled: true, sasl_mechanism: "scram_sha_256" };
    expect(syncForm({ form: secured })).toEqual(secured);
  });

  it("uses the atomic lifecycle for a TLS Redpanda target", async () => {
    const form = {
      ...emptyForm(),
      server_family: "redpanda",
      tls_enabled: true,
      tls_server_name: "broker.example.test",
      tls_ca_pem: "test-ca",
      sasl_mechanism: "scram_sha_256",
      username: "service",
      password: "test-password",
    };
    await save({ mode: "create", form });
    expect(api.post).toHaveBeenCalledWith(
      "/api/connector-targets/with-profile",
      expect.objectContaining({
        target: expect.objectContaining({
          connector_kind: "kafka",
          config: expect.objectContaining({
            server_family: "redpanda",
            tls_enabled: true,
            tls_server_name: "broker.example.test",
            tls_ca_pem: "test-ca",
            transport_target_ref: "",
          }),
        }),
        profile: expect.objectContaining({
          kind: "sasl",
          public: { mechanism: "scram_sha_256", username: "service" },
          secret: { password: "test-password" },
        }),
      }),
    );
  });

  it("keeps profile projection public and preserves stored SASL passwords on edit", async () => {
    const profile = {
      id: 9,
      kind: "sasl",
      label: "monitor",
      public: { mechanism: "scram_sha_256", username: "reader" },
      risk_label: "read",
    };
    const target: KafkaTarget = {
      id: 3,
      connector_kind: "kafka",
      name: "stream",
      config: { bootstrap_brokers: "broker-a:9092, broker-b:9092", connection_mode: "over_ssh" },
      profiles: [profile],
    };
    expect(formFromTarget({ target })).toMatchObject({
      profile_id: "9",
      sasl_mechanism: "scram_sha_256",
      username: "reader",
      password: "",
    });
    const credentialState = credentialStateFromRow({ row: { target_id: 3, name: "monitor", profile } });
    expect(credentialState.form.password).toBe("");
    await saveCredential({ operation: "update", row: { id: 9, target_id: 3, profile }, formState: credentialState });
    expect(api.put).toHaveBeenCalledWith("/api/connector-targets/3/profiles/9", {
      kind: "sasl",
      label: "monitor",
      public: { mechanism: "scram_sha_256", username: "reader" },
      risk_label: "read",
    });
    expect(targetEndpoint({ target })).toBe("broker-a:9092, broker-b:9092 · over ssh");
    expect(emptyCredentialState({ targets: [target] }).form.target_id).toBe("3");
    expect(credentialRows({ targets: [target] })[0].metadata).toEqual(["SASL: scram_sha_256", "username: reader", "risk: read"]);
  });

  it("rejects enabling SASL without a password before any credential mutation", async () => {
    await expect(
      saveCredential({
        operation: "create",
        formState: {
          form: {
            target_id: "3",
            profile_label: "service",
            sasl_mechanism: "plain",
            existing_sasl_mechanism: "none",
            username: "reader",
            password: "",
            risk_label: "read",
          },
        },
      }),
    ).rejects.toThrow("Password is required");
    expect(api.post).not.toHaveBeenCalled();
  });

  it("explicitly clears the secret and username when SASL is disabled", async () => {
    await saveCredential({
      operation: "update",
      row: { id: 9, target_id: 3, profile: { id: 9, label: "monitor", kind: "sasl" } },
      formState: {
        form: {
          target_id: "3",
          profile_label: "monitor",
          sasl_mechanism: "none",
          existing_sasl_mechanism: "plain",
          username: "old",
          password: "",
          risk_label: "read",
        },
      },
    });
    expect(api.put).toHaveBeenCalledWith("/api/connector-targets/3/profiles/9", {
      kind: "sasl",
      label: "monitor",
      public: { mechanism: "none", username: "" },
      secret: { password: "" },
      risk_label: "read",
    });
  });
});
