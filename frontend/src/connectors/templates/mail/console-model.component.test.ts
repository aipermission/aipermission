import { describe, expect, it } from "vitest";
import { mailConsoleModel } from "./console-model";

describe("Mail console presentation", () => {
  it("retains endpoint, transport and target/profile identity without persistence IDs", () => {
    const target = {
      ref: "mail:3:7",
      connector_kind: "mail",
      target_name: "Support",
      profile_label: "Mailbox",
      config: { imap_host: "imap.local", imap_port: "993", smtp_host: "smtp.local", smtp_port: 587, connection_mode: "over_ssh" },
      public: { mailbox_address: "support@example.com", allowed_read_folders: ["INBOX"] },
    };
    expect(mailConsoleModel.targetDisplayName({ target })).toBe("Support");
    expect(mailConsoleModel.targetSubtitle({ target })).toBe("IMAP imap.local:993 · SMTP smtp.local:587 · over ssh");
    expect(mailConsoleModel.targetProfileLabel({ target })).toBe("Mailbox");
    expect(mailConsoleModel.usesLiveConsole({ target })).toBe(false);
    expect(mailConsoleModel.recoverableRunningActions({ target })).toEqual([]);
  });

  it("retains missing-target and empty-config defaults", () => {
    expect(mailConsoleModel.targetDisplayName({})).toBe("Mail target");
    expect(mailConsoleModel.targetProfileLabel({ target: null })).toBe("mailbox");
    expect(mailConsoleModel.targetSubtitle({ target: { ref: "mail:3:7", connector_kind: "mail" } })).toBe(
      "IMAP host:993 · SMTP host:465 · direct",
    );
  });

  it("rejects malformed native endpoint or public profile data", () => {
    const target = { ref: "mail:3:7", connector_kind: "mail" };
    expect(() => mailConsoleModel.targetSubtitle({ target: { ...target, config: { imap_port: false } } })).toThrow(
      "Invalid Mail IMAP console target port.",
    );
    expect(() => mailConsoleModel.targetProfileLabel({ target: { ...target, public: { mailbox_address: 123 } } })).toThrow(
      "Invalid Mail console target mailbox_address.",
    );
  });
});
