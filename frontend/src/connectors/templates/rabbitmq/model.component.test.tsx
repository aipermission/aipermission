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
import type { RabbitMQTarget } from "./form-types";

const api = vi.hoisted(() => ({ post: vi.fn(), put: vi.fn() }));
vi.mock("../../../lib/api.ts", () => ({ apiPost: api.post, apiPut: api.put, apiDelete: vi.fn() }));

describe("RabbitMQ credential lifecycle model", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.post.mockResolvedValue({ id: 3, profiles: [] });
    api.put.mockResolvedValue({ id: 3, profiles: [] });
  });

  it("uses the shared atomic lifecycle and normalizes management connection fields", async () => {
    const form = {
      ...emptyForm(),
      project_id: 2,
      connection_mode: "over_ssh",
      transport_target_ref: "ssh:1:1",
      scheme: "https",
      port: "15671",
      password: "test-password",
    };
    await save({ mode: "create", form });
    expect(api.post).toHaveBeenCalledWith("/api/connector-targets/with-profile", {
      target: {
        connector_kind: "rabbitmq",
        name: "rabbitmq",
        project_id: 2,
        config: {
          connection_mode: "over_ssh",
          scheme: "https",
          host: "127.0.0.1",
          port: 15671,
          vhost: "/",
          transport_target_ref: "ssh:1:1",
        },
      },
      profile: {
        kind: "username_password",
        label: "monitor",
        public: { username: "" },
        secret: { password: "test-password" },
        risk_label: "queue access",
      },
    });
    expect(syncForm({ form: { ...form, connection_mode: "direct", scheme: "", vhost: "" } })).toMatchObject({
      transport_target_ref: "",
      scheme: "http",
      vhost: "/",
    });
    expect(syncForm({ form })).toEqual(form);
  });

  it("projects only public profile metadata and retains explicit profile selection", () => {
    const target: RabbitMQTarget = {
      id: 3,
      name: "queue",
      connector_kind: "rabbitmq",
      config: { host: "queue.example.test", port: 15671, scheme: "https", vhost: "jobs" },
      profiles: [
        { id: 4, label: "read", kind: "username_password", public: { username: "reader" }, risk_label: "read" },
        { id: 5, label: "write", kind: "username_password" },
      ],
    };
    expect(formFromTarget({ target })).toMatchObject({ profile_id: "", username: "", password: "" });
    expect(formFromTarget({ target, profile: target.profiles?.[0] })).toMatchObject({ profile_id: "4", username: "reader", password: "" });
    expect(credentialStateFromRow({ row: { target_id: 3, name: "read", profile: target.profiles?.[0] } })).toEqual({
      form: { target_id: "3", profile_label: "read", username: "reader", password: "", risk_label: "read" },
    });
    expect(emptyCredentialState({ targets: [target] }).form.target_id).toBe("3");
    expect(targetEndpoint({ target })).toBe("https://queue.example.test:15671 · vhost jobs · direct");
    expect(credentialRows({ targets: [target] })).toEqual([
      expect.objectContaining({ id: 4, metadata: ["username: reader", "risk: read"] }),
      expect.objectContaining({ id: 5, metadata: ["No public metadata"] }),
    ]);
  });

  it("does not replace an encrypted password when an edited credential leaves it blank", async () => {
    await saveCredential({
      operation: "update",
      row: { id: 4, target_id: 3, profile: { id: 4, label: "read", kind: "username_password" } },
      formState: { form: { target_id: "3", profile_label: "read", username: "reader", password: "", risk_label: "read" } },
    });
    expect(api.put).toHaveBeenCalledWith("/api/connector-targets/3/profiles/4", {
      kind: "username_password",
      label: "read",
      public: { username: "reader" },
      risk_label: "read",
    });
  });
});
