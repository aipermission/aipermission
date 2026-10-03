import { expect, it } from "vitest";
import { gatewayTargetFixture } from "../../../test/connector-inventory-fixtures";
import { mailConsoleTarget, mailPolicyList } from "./console-target";

it("preserves Mail protocol capabilities, folder policy and endpoint metadata", () => {
  const target = gatewayTargetFixture({
    connector_kind: "mail",
    ref: "mail:3:11",
    profile_label: "Mailbox",
    config: { imap_host: "imap.test", imap_port: 993, smtp_host: "smtp.test", smtp_port: "465", connection_mode: "over_ssh" },
    public: {
      mailbox_address: "operator@example.test",
      display_name: "Operator",
      reply_to: "reply@example.test",
      imap_enabled: false,
      smtp_auth_mode: "separate",
      allowed_read_folders: ["INBOX"],
      allowed_mutation_source_folders: "INBOX",
      allowed_mutation_destination_folders: ["Archive", "Trash"],
      sent_folder: "Sent",
      archive_folder: "Archive",
      trash_folder: "Trash",
      ignored: true,
    },
  });
  const projected = mailConsoleTarget(target);
  expect(projected.config).toEqual(target.config);
  expect(projected.public).toEqual({
    mailbox_address: "operator@example.test",
    display_name: "Operator",
    reply_to: "reply@example.test",
    imap_enabled: false,
    smtp_auth_mode: "separate",
    allowed_read_folders: ["INBOX"],
    allowed_mutation_source_folders: "INBOX",
    allowed_mutation_destination_folders: ["Archive", "Trash"],
    sent_folder: "Sent",
    archive_folder: "Archive",
    trash_folder: "Trash",
  });
  expect(projected.public?.allowed_mutation_destination_folders).not.toBe(target.public?.allowed_mutation_destination_folders);
  expect(projected.profile_label).toBe("Mailbox");
  expect(projected.ref).toBe("mail:3:11");
  expect(mailConsoleTarget({ ...target, config: undefined, public: undefined }).public?.imap_enabled).toBeUndefined();
});

it.each(["imap_host", "imap_port", "smtp_host", "smtp_port", "connection_mode"])("rejects malformed endpoint %s", (field) => {
  expect(() => mailConsoleTarget(gatewayTargetFixture({ config: { [field]: {} } }))).toThrow("Invalid Mail");
});

it.each([
  "mailbox_address",
  "display_name",
  "reply_to",
  "imap_enabled",
  "smtp_auth_mode",
  "allowed_read_folders",
  "allowed_mutation_source_folders",
  "allowed_mutation_destination_folders",
  "sent_folder",
  "archive_folder",
  "trash_folder",
])("rejects malformed public profile %s", (field) => {
  expect(() => mailConsoleTarget(gatewayTargetFixture({ public: { [field]: {} } }))).toThrow(`Invalid Mail console target ${field}.`);
});

it.each([undefined, null, "", ",\n", [], "INBOX,\nSent", ["INBOX", "Sent"]])("preserves supported policy list %j", (value) => {
  expect(mailPolicyList(value, "allowed_read_folders")).toEqual(value === null ? undefined : value);
});

it.each([true, 1, ["INBOX", null], ["INBOX", 3], " ", ["INBOX", " "]])("rejects malformed policy list %j", (value) => {
  expect(() => mailPolicyList(value, "allowed_read_folders")).toThrow("Invalid Mail console target allowed_read_folders.");
});
