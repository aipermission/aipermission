import {
  optionalConsoleBoolean,
  optionalConsolePort,
  optionalConsoleText,
  optionalConsoleTextList,
} from "../_shared/console-target-config";
import type { ConsolePresentationTarget } from "../_shared/console-presentation-types";
import type { MailTarget } from "./form-types";
import type { MailWorkspaceTarget } from "./use-mail-workspace";

export type MailConsoleTarget = MailTarget & MailWorkspaceTarget;

export function mailConsoleTarget(target: ConsolePresentationTarget): MailConsoleTarget {
  const config = target.config || {};
  const profile = target.public || {};
  return {
    ref: target.ref,
    profile_label: target.profile_label,
    config: {
      imap_host: optionalConsoleText(config.imap_host, "Mail", "imap_host"),
      imap_port: optionalConsolePort(config.imap_port, "Mail IMAP"),
      smtp_host: optionalConsoleText(config.smtp_host, "Mail", "smtp_host"),
      smtp_port: optionalConsolePort(config.smtp_port, "Mail SMTP"),
      connection_mode: optionalConsoleText(config.connection_mode, "Mail", "connection_mode"),
    },
    public: mailPublicProfile(profile),
  };
}

export function mailPublicProfile(profile: Record<string, unknown>) {
  return {
    mailbox_address: optionalConsoleText(profile.mailbox_address, "Mail", "mailbox_address"),
    display_name: optionalConsoleText(profile.display_name, "Mail", "display_name"),
    reply_to: optionalConsoleText(profile.reply_to, "Mail", "reply_to"),
    imap_enabled: optionalConsoleBoolean(profile.imap_enabled, "Mail", "imap_enabled"),
    smtp_auth_mode: optionalConsoleText(profile.smtp_auth_mode, "Mail", "smtp_auth_mode"),
    allowed_read_folders: mailPolicyList(profile.allowed_read_folders, "allowed_read_folders"),
    allowed_mutation_source_folders: mailPolicyList(profile.allowed_mutation_source_folders, "allowed_mutation_source_folders"),
    allowed_mutation_destination_folders: mailPolicyList(
      profile.allowed_mutation_destination_folders,
      "allowed_mutation_destination_folders",
    ),
    sent_folder: optionalConsoleText(profile.sent_folder, "Mail", "sent_folder"),
    archive_folder: optionalConsoleText(profile.archive_folder, "Mail", "archive_folder"),
    trash_folder: optionalConsoleText(profile.trash_folder, "Mail", "trash_folder"),
  };
}

export function mailPolicyList(value: unknown, field: string) {
  const list = optionalConsoleTextList(value === null ? undefined : value, "Mail", field);
  const entries = typeof list === "string" ? list.split(/[,\n]/).filter((item) => item !== "") : list;
  if (entries?.some((item) => item.trim() === "")) throw new Error(`Invalid Mail console target ${field}.`);
  return list;
}
