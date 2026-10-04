import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiPost, apiPut } from "../../../lib/api.ts";
import { emptyForm, formFromTarget, hostKeyActionFromError, operationFromError, resumeHostKeyAction, save } from "./model";
import {
  isHostKeyError,
  keyNameFromFilename,
  payloadFromForm,
  profilePublicFromPayload,
  sshCredentialResourcesResponse,
  targetConfigFromPayload,
} from "./model-helpers";

const hostKey = {
  host: "example.test",
  hostname: "example.test:22",
  port: 22,
  public_key: "ssh-ed25519 fixture",
  fingerprint_sha256: "SHA256:fixture",
  key_type: "ssh-ed25519",
};
const conflict = { status: 409, data: { code: "unknown_ssh_host_key", host_key: hostKey } };
const config = { host: "example.test", port: 22, description: "", startup_input_after_connect: "", force_shell_command: "" };

vi.mock("../../../lib/api.ts", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiGet: vi.fn(), apiDelete: vi.fn() }));
beforeEach(() => vi.resetAllMocks());

describe("SSH model boundaries", () => {
  it("uses the shared atomic owner for deferred setup and the exact selected profile", async () => {
    const target = { id: 3, name: "Example", connector_kind: "ssh", profiles: [{ id: 9 }, { id: 10 }] };
    const form = { ...emptyForm({ firstCredentialID: 7 }), name: "Example", host: "example.test", project_id: 2, setup_later: true };
    vi.mocked(apiPost).mockResolvedValueOnce(target);
    await save({ mode: "create", form });
    expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/with-profile", {
      target: { connector_kind: "ssh", name: "Example", config, project_id: 2 },
      profile: { kind: "private_key", label: "root", public: { username: "root", ssh_key_id: 7 } },
    });
    vi.mocked(apiPut).mockResolvedValueOnce(target);
    await save({ mode: "edit", form: { ...form, profile_id: "10" }, target });
    expect(apiPut).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/with-profile/10", {
      target: { name: "Example", config, project_id: 2 },
      profile: { kind: "private_key", label: "root", public: { username: "root", ssh_key_id: 7 } },
    });
    expect(apiPost).toHaveBeenCalledTimes(1);
  });

  it("propagates uncertain atomic persistence without retrying custom provisioning", async () => {
    const failure = new Error("mutation outcome unknown");
    vi.mocked(apiPost).mockRejectedValueOnce(failure);
    await expect(save({ mode: "create", form: { ...emptyForm({ firstCredentialID: 7 }), setup_later: true } })).rejects.toBe(failure);
    expect(apiPost).toHaveBeenCalledTimes(1);
    expect(apiPut).not.toHaveBeenCalled();
  });

  it("validates host-key metadata before exposing recovery operations", () => {
    expect(isHostKeyError(conflict)).toBe(true);
    for (const value of [
      null,
      {},
      { ...conflict, status: 500 },
      { ...conflict, data: { ...conflict.data, host_key: {} } },
      { ...conflict, data: { ...conflict.data, host_key: { ...hostKey, port: "22" } } },
      { ...conflict, data: { ...conflict.data, host_key: { ...hostKey, changed: "true" } } },
      { ...conflict, data: { ...conflict.data, host_key: { ...hostKey, existing_fingerprints: [1] } } },
    ]) {
      expect(isHostKeyError(value)).toBe(false);
      expect(operationFromError(value, { operation: "test", testKey: "fixture" })).toBeNull();
    }
    expect(operationFromError(conflict, { operation: "test", testKey: "fixture" })).toMatchObject({
      connector_kind: "ssh",
      type: "host-key",
      hostKey,
      action: { type: "test", testKey: "fixture" },
    });
  });

  it("withholds form recovery when its source form is missing", async () => {
    expect(hostKeyActionFromError(conflict, {})).toBeNull();
    await expect(resumeHostKeyAction({ kind: "ssh", type: "create" })).rejects.toThrow("form is not loaded");
    await expect(resumeHostKeyAction({ kind: "ssh", type: "save" })).rejects.toThrow("form is not loaded");
    await expect(resumeHostKeyAction({ kind: "ssh", type: "test" })).rejects.toThrow("profile is not loaded");
  });

  it("separates target configuration and profile key references", () => {
    const form = { ...emptyForm({ firstCredentialID: 7 }), name: "Example", host: "example.test", profile_id: "9" };
    const payload = payloadFromForm(form);
    expect(profilePublicFromPayload(payload)).toEqual({ username: "root", ssh_key_id: 7 });
    expect(targetConfigFromPayload(payload)).toEqual(config);
    expect(payload.profile_id).toBe(9);
    expect(
      formFromTarget({
        target: {
          id: 1,
          name: "Example",
          connector_kind: "ssh",
          profiles: [{ id: 9, public: { username: "operator", ssh_key_id: 7 } }],
          config: { host: "example.test" },
        },
      }),
    ).toMatchObject({ username: "operator", ssh_key_id: "7", profile_id: "9" });
  });

  it("parses both gateway credential envelopes and lists without accepting malformed keys", () => {
    const row = { id: 7, name: "Main", key_type: "ed25519", install_command: "fixture" };
    expect(sshCredentialResourcesResponse([row])).toEqual(sshCredentialResourcesResponse({ items: [row] }));
    expect(sshCredentialResourcesResponse([row])[0]).toMatchObject({ resource_ref: "ssh:ssh_key:7" });
    for (const value of [
      null,
      {},
      { items: null },
      [{ ...row, id: "7" }],
      [{ ...row, key_type: null }],
      [{ ...row, install_command: 1 }],
    ]) {
      expect(() => sshCredentialResourcesResponse(value)).toThrow("Invalid SSH credential resources");
    }
  });

  it("sanitizes imported key labels and retains the empty-name fallback", () => {
    expect(keyNameFromFilename("project/key.pem", "imported-key")).toBe("project-key");
    expect(keyNameFromFilename(".pem", "imported-key")).toBe("imported-key");
    expect(keyNameFromFilename("a".repeat(100), "imported-key")).toHaveLength(80);
  });
});
