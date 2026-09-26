import { beforeEach, describe, expect, it, vi } from "vitest";
import { credentialRows, credentialStateFromRow, emptyForm, formFromTarget, save, saveCredential, syncForm } from "./model";
import type { MailTarget } from "./form-types";

const api = vi.hoisted(() => ({ post: vi.fn(), put: vi.fn() }));
vi.mock("../../../lib/api.ts", () => ({ apiPost: api.post, apiPut: api.put, apiDelete: vi.fn() }));

describe("Mail credential lifecycle model", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.post.mockResolvedValue({ id: 3, profiles: [] });
    api.put.mockResolvedValue({ id: 3, profiles: [] });
  });

  it("keeps login secrets separate from public folder policies in atomic creation", async () => {
    const form = {
      ...emptyForm(), project_id: 3, imap_port: "993", smtp_port: "465",
      imap_username: "reader", imap_password: "test-imap-password",
      smtp_auth_mode: "separate", smtp_username: "sender", smtp_password: "test-smtp-password",
      allowed_read_folders: "INBOX,Sent\n Archive ", allowed_recipient_domains: "example.test, example.org",
    };
    await save({ mode: "create", form });
    expect(api.post).toHaveBeenCalledTimes(1);
    expect(api.put).not.toHaveBeenCalled();
    expect(api.post).toHaveBeenCalledWith("/api/connector-targets/with-profile", {
      target: { connector_kind: "mail", name: "mailbox", project_id: 3, config: {
        connection_mode: "direct", transport_target_ref: "", imap_host: "imap.example.com", imap_port: 993,
        imap_tls_mode: "implicit_tls", smtp_host: "smtp.example.com", smtp_port: 465,
        smtp_tls_mode: "implicit_tls", allowed_recipient_domains: ["example.test", "example.org"],
      } },
      profile: { kind: "password", label: "mailbox", risk_label: "mailbox access", public: {
        mailbox_address: "", display_name: "", reply_to: "", imap_enabled: true, smtp_auth_mode: "separate",
        allowed_read_folders: ["INBOX", "Sent", "Archive"], allowed_mutation_source_folders: ["INBOX"],
        allowed_mutation_destination_folders: [], sent_folder: "", archive_folder: "", trash_folder: "",
      }, secret: { imap_username: "reader", imap_password: "test-imap-password", smtp_username: "sender", smtp_password: "test-smtp-password" } },
    });
  });

  it("does not guess a profile or expose saved secrets in edit state", () => {
    const savedProfile = { id: 4, label: "read", kind: "password", public: { mailbox_address: "reader@example.test", allowed_read_folders: ["INBOX", "Sent"] },
      secret: { imap_username: "secret-reader", imap_password: "secret-imap", smtp_username: "secret-sender", smtp_password: "secret-smtp" } };
    const target: MailTarget = { id: 2, name: "mail", connector_kind: "mail", profiles: [
      savedProfile,
      { id: 5, label: "send", kind: "password", public: { imap_enabled: false, smtp_auth_mode: "separate" } },
    ] };
    expect(formFromTarget({ target }).profile_id).toBe("");
    expect(formFromTarget({ target, profile: target.profiles?.[0] })).toMatchObject({
      profile_id: "4", allowed_read_folders: "INBOX\nSent", imap_username: "", imap_password: "", smtp_username: "", smtp_password: "",
    });
    expect(credentialStateFromRow({ row: { target_id: 2, profile: target.profiles?.[0] } }).form.mailbox_address).toBe("reader@example.test");
    expect(credentialRows({ targets: [target] })[1].metadata).toEqual(["IMAP disabled", "SMTP: separate"]);
  });

  it("preserves existing encrypted credentials when every login input stays blank", async () => {
    const form = { ...emptyForm(), target_id: "2" };
    await saveCredential({ operation: "update", row: { id: 4, target_id: 2, profile: { id: 4, label: "old", kind: "password" } }, formState: { form } });
    expect(api.put).toHaveBeenCalledWith("/api/connector-targets/2/profiles/4", expect.not.objectContaining({ secret: expect.anything() }));
    expect(api.put.mock.calls[0][1].public).not.toHaveProperty("imap_password");
    expect(api.put.mock.calls[0][1]).not.toHaveProperty("secret");
    api.put.mockClear();
    const profile = { id: 4, label: "old", kind: "password" };
    const target: MailTarget = { id: 2, name: "mail", connector_kind: "mail", profiles: [profile] };
    await save({ mode: "edit", form: formFromTarget({ target, profile }), target });
    expect(api.put).toHaveBeenCalledTimes(1);
    expect(api.put.mock.calls[0][0]).toBe("/api/connector-targets/2/with-profile/4");
    expect(api.put.mock.calls[0][1].profile).not.toHaveProperty("secret");
  });

  it("rejects a disabled protocol configuration before making a request", async () => {
    const form = { ...emptyForm(), target_id: "2", imap_enabled: false, smtp_auth_mode: "disabled" };
    await expect(save({ mode: "create", form })).rejects.toThrow("Enable IMAP or SMTP");
    await expect(saveCredential({ operation: "create", formState: { form }, targets: [] })).rejects.toThrow("Enable IMAP or SMTP");
    expect(api.post).not.toHaveBeenCalled();
    expect(api.put).not.toHaveBeenCalled();
    expect(syncForm({ form: { ...form, smtp_auth_mode: "reuse_imap", transport_target_ref: "ssh:1:1" } })).toMatchObject({ smtp_auth_mode: "disabled", transport_target_ref: "" });
  });
});
