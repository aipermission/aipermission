import { defineCredentialFamily } from "../_shared/credential-family-registration";
import { credentialDisplayRow } from "../_shared/credential-display-row";
import { optionalConsolePort, optionalConsoleText, optionalConsoleTextList } from "../_shared/console-target-config";
import type { InventoryTarget } from "../../../lib/gateway-contracts/connector-inventory-contract";
import { MailCredentialFormTemplate } from "./credential-form";
import { mailPublicProfile } from "./console-target";
import * as model from "./model";

type Target = ReturnType<typeof mailCredentialTargets>[number];
type State = ReturnType<typeof model.emptyCredentialState>;
type Profile = Target["profiles"][number];
type Row = ReturnType<typeof model.credentialRows<Profile, Target>>[number];

export const mailCredentialFamily = defineCredentialFamily<State, Row, Target, "create" | "update">({
  kind: "mail",
  label: "Mail",
  decodeTargets: mailCredentialTargets,
  emptyState: model.emptyCredentialState,
  model,
  rows: ({ targets }) => model.credentialRows<Profile, Target>({ targets }),
  displayRow: credentialDisplayRow,
  renderForm: ({ editor, targets }) => (
    <MailCredentialFormTemplate
      {...model.credentialFormProps({
        targets,
        formState: editor.formState,
        setFormState: editor.setFormState,
        formMode: editor.drawer.mode,
        state: editor.actionState,
        onSubmit: editor.save,
      })}
    />
  ),
});

export function mailCredentialTargets(targets: readonly InventoryTarget[]) {
  return targets
    .filter((target) => target.connector_kind === "mail")
    .map((target) => {
      const config = target.config || {};
      return {
        ...target,
        config: {
          ...config,
          imap_host: optionalConsoleText(config.imap_host, "Mail", "imap_host"),
          imap_port: optionalConsolePort(config.imap_port, "Mail IMAP"),
          imap_tls_mode: optionalConsoleText(config.imap_tls_mode, "Mail", "imap_tls_mode"),
          smtp_host: optionalConsoleText(config.smtp_host, "Mail", "smtp_host"),
          smtp_port: optionalConsolePort(config.smtp_port, "Mail SMTP"),
          smtp_tls_mode: optionalConsoleText(config.smtp_tls_mode, "Mail", "smtp_tls_mode"),
          connection_mode: optionalConsoleText(config.connection_mode, "Mail", "connection_mode"),
          transport_target_ref: optionalConsoleText(config.transport_target_ref, "Mail", "transport_target_ref"),
          allowed_recipient_domains: optionalConsoleTextList(config.allowed_recipient_domains, "Mail", "allowed_recipient_domains"),
        },
        profiles: (target.profiles || []).map((profile) => ({
          ...profile,
          public: { ...profile.public, ...mailPublicProfile(profile.public || {}) },
        })),
      };
    });
}
