import type { ComponentProps } from "react";
import type { CredentialProfileForm } from "../_shared/connector-form-types";
import type { ConnectionModeFields } from "../_shared/network-transport-fields";

export type MailProfileForm = Omit<CredentialProfileForm, "target_id"> & {
  mailbox_address: string;
  display_name: string;
  reply_to: string;
  imap_enabled: boolean;
  imap_username: string;
  imap_password: string;
  smtp_auth_mode: string;
  smtp_username: string;
  smtp_password: string;
  allowed_read_folders: string;
  allowed_mutation_source_folders: string;
  allowed_mutation_destination_folders: string;
  sent_folder: string;
  archive_folder: string;
  trash_folder: string;
};
export type MailCredentialForm = MailProfileForm & Pick<CredentialProfileForm, "target_id">;
type MailEndpointFields = { [_Field in `${"imap" | "smtp"}_${"host" | "tls_mode"}`]: string } & {
  imap_port: string | number;
  smtp_port: string | number;
};
export type MailConnectionForm = MailProfileForm &
  ComponentProps<typeof ConnectionModeFields>["form"] &
  MailEndpointFields & {
    name: string;
    project_id?: string | number;
    allowed_recipient_domains: string;
  };
