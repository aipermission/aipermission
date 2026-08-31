import { connectorCredentialRows, createTargetProfileLifecycle } from "../_shared/target-profile-lifecycle";
import { mailProtocolsEnabled } from "./helpers";

const emptyMailCredentialForm = {
  target_id: "",
  profile_label: "mailbox",
  mailbox_address: "",
  display_name: "",
  reply_to: "",
  imap_enabled: true,
  imap_username: "",
  imap_password: "",
  smtp_auth_mode: "disabled",
  smtp_username: "",
  smtp_password: "",
  allowed_read_folders: "INBOX",
  allowed_mutation_source_folders: "INBOX",
  allowed_mutation_destination_folders: "",
  sent_folder: "",
  archive_folder: "",
  trash_folder: "",
  risk_label: "mailbox access",
};
const lifecycle = createTargetProfileLifecycle({
  connectorKind: "mail",
  connectorLabel: "Mail",
  targetPayload: (form) => ({ name: form.name, config: targetConfig(form) }),
  profilePayload: (form, { profile, operation }) => credentialPayload(form, operation.endsWith("create"), profile),
  beforeSave: ({ form }) => requireMailProtocol(form, "connector"),
  beforeSaveCredential: ({ form }) => requireMailProtocol(form, "credential"),
});

export const { credentialFormProps, deleteCredential, deleteTarget, save, saveCredential, test } = lifecycle;

export function emptyForm() {
  return {
    connector_kind: "mail",
    name: "mailbox",
    connection_mode: "direct",
    transport_target_ref: "",
    imap_host: "imap.example.com",
    imap_port: 993,
    imap_tls_mode: "implicit_tls",
    smtp_host: "smtp.example.com",
    smtp_port: 465,
    smtp_tls_mode: "implicit_tls",
    allowed_recipient_domains: "",
    ...emptyMailCredentialForm,
  };
}

export function formFromTarget({ target, profile }) {
  const selectedProfile = profile || (target?.profiles?.length === 1 ? target.profiles[0] : {});
  return {
    connector_kind: "mail",
    profile_id: selectedProfile.id ? String(selectedProfile.id) : "",
    name: target.name || "",
    connection_mode: target.config?.connection_mode || "direct",
    transport_target_ref: target.config?.transport_target_ref || "",
    imap_host: target.config?.imap_host || "",
    imap_port: target.config?.imap_port || 993,
    imap_tls_mode: target.config?.imap_tls_mode || "implicit_tls",
    smtp_host: target.config?.smtp_host || "",
    smtp_port: target.config?.smtp_port || 465,
    smtp_tls_mode: target.config?.smtp_tls_mode || "implicit_tls",
    allowed_recipient_domains: listText(target.config?.allowed_recipient_domains),
    ...credentialFormFromProfile(selectedProfile),
  };
}

export function activeCredential() {
  return null;
}

export function syncForm({ form }) {
  if (form.connector_kind !== "mail") return form;
  const next = { ...form };
  if (next.connection_mode === "direct") next.transport_target_ref = "";
  if (!next.imap_tls_mode) next.imap_tls_mode = "implicit_tls";
  if (!next.smtp_tls_mode) next.smtp_tls_mode = "implicit_tls";
  if (!next.smtp_auth_mode) next.smtp_auth_mode = "disabled";
  if (next.imap_enabled === false && next.smtp_auth_mode === "reuse_imap") next.smtp_auth_mode = "disabled";
  return next;
}

export function submitDisabled({ state, form }) {
  return state.state === "saving" || !mailProtocolsEnabled(form);
}

export function submitLabel({ state, mode }) {
  if (state.state === "saving") return "Saving...";
  return mode === "edit" ? "Save changes" : "Create connector";
}

export function emptyCredentialState({ targets = [] } = {}) {
  const firstTarget = targets.find((target) => target.connector_kind === "mail");
  return { form: { ...emptyMailCredentialForm, target_id: String(firstTarget?.id || "") } };
}

export function credentialStateFromRow({ row }) {
  return { form: { target_id: String(row.target_id || ""), ...credentialFormFromProfile(row.profile) } };
}

export function credentialRows({ targets }) {
  return connectorCredentialRows({ targets, connectorKind: "mail", connectorLabel: "Mail", targetEndpoint, credentialMetadata });
}

export function canEdit() {
  return true;
}

export function canDelete() {
  return true;
}

export function credentialHint() {
  return null;
}

export function targetEndpoint({ target }) {
  const mode = target.config?.connection_mode === "over_ssh" ? "over ssh" : "direct";
  return `IMAP ${target.config?.imap_host || "host"}:${target.config?.imap_port || 993} · SMTP ${target.config?.smtp_host || "host"}:${target.config?.smtp_port || 465} · ${mode}`;
}

export function targetDisplayName({ target }) {
  return target?.target_name || target?.name || "Mail target";
}

export function targetSubtitle({ target }) {
  return targetEndpoint({ target });
}

export function targetProfileLabel({ target }) {
  return target?.profile_label || "mailbox";
}

export function usesLiveConsole() {
  return false;
}

export function recoverableRunningActions() {
  return [];
}

export function deleteDialog({ target }) {
  return {
    title: target ? `Delete ${target.name}` : "Delete connector",
    description: "Remove this Mail connector target, credential profiles, and token action permissions from aipermission.",
    details: [
      { label: "Connector", value: target?.name },
      { label: "Reference", value: target ? `${target.connector_kind}:${target.id}` : "" },
    ],
    notice: "This removes local connector configuration. It does not delete mailbox messages or change the mail server.",
    actions: [
      { label: "Cancel", action: "close", variant: "outline" },
      { label: "Delete connector", pendingLabel: "Deleting...", removeKey: false },
    ],
  };
}

export function operationFromError() {
  return null;
}

function targetConfig(form) {
  return {
    connection_mode: form.connection_mode || "direct",
    transport_target_ref: form.connection_mode === "over_ssh" ? form.transport_target_ref || "" : "",
    imap_host: form.imap_host,
    imap_port: Number(form.imap_port) || 993,
    imap_tls_mode: form.imap_tls_mode || "implicit_tls",
    smtp_host: form.smtp_host,
    smtp_port: Number(form.smtp_port) || 465,
    smtp_tls_mode: form.smtp_tls_mode || "implicit_tls",
    allowed_recipient_domains: lineList(form.allowed_recipient_domains),
  };
}

function requireMailProtocol(form, resource) {
  if (!mailProtocolsEnabled(form)) throw new Error(`Enable IMAP or SMTP before saving this Mail ${resource}.`);
}

function credentialPayload(form, creating, existing = null) {
  const payload = {
    kind: existing?.kind || "password",
    label: form.profile_label,
    public: {
      mailbox_address: form.mailbox_address,
      display_name: form.display_name || "",
      reply_to: form.reply_to || "",
      imap_enabled: Boolean(form.imap_enabled),
      smtp_auth_mode: form.smtp_auth_mode || "disabled",
      allowed_read_folders: lineList(form.allowed_read_folders),
      allowed_mutation_source_folders: lineList(form.allowed_mutation_source_folders),
      allowed_mutation_destination_folders: lineList(form.allowed_mutation_destination_folders),
      sent_folder: form.sent_folder || "",
      archive_folder: form.archive_folder || "",
      trash_folder: form.trash_folder || "",
    },
    risk_label: form.risk_label || "mailbox access",
  };
  const secret = {};
  if (form.imap_username) secret.imap_username = form.imap_username;
  if (form.imap_password) secret.imap_password = form.imap_password;
  if (form.smtp_username) secret.smtp_username = form.smtp_username;
  if (form.smtp_password) secret.smtp_password = form.smtp_password;
  if (creating || Object.keys(secret).length > 0) payload.secret = secret;
  return payload;
}

function credentialFormFromProfile(profile = {}) {
  return {
    profile_label: profile.label || "mailbox",
    mailbox_address: profile.public?.mailbox_address || "",
    display_name: profile.public?.display_name || "",
    reply_to: profile.public?.reply_to || "",
    imap_enabled: profile.public?.imap_enabled !== false,
    imap_username: "",
    imap_password: "",
    smtp_auth_mode: profile.public?.smtp_auth_mode || "disabled",
    smtp_username: "",
    smtp_password: "",
    allowed_read_folders: listText(profile.public?.allowed_read_folders || ["INBOX"]),
    allowed_mutation_source_folders: listText(profile.public?.allowed_mutation_source_folders || ["INBOX"]),
    allowed_mutation_destination_folders: listText(profile.public?.allowed_mutation_destination_folders || []),
    sent_folder: profile.public?.sent_folder || "",
    archive_folder: profile.public?.archive_folder || "",
    trash_folder: profile.public?.trash_folder || "",
    risk_label: profile.risk_label || "mailbox access",
  };
}

function lineList(value) {
  if (Array.isArray(value)) return value.map((item) => String(item).trim()).filter(Boolean);
  return String(value || "")
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function listText(value) {
  return Array.isArray(value) ? value.join("\n") : String(value || "");
}

function credentialMetadata(profile) {
  const items = [];
  if (profile.public?.mailbox_address) items.push(`mailbox: ${profile.public.mailbox_address}`);
  items.push(profile.public?.imap_enabled === false ? "IMAP disabled" : "IMAP enabled");
  items.push(`SMTP: ${profile.public?.smtp_auth_mode || "disabled"}`);
  if (profile.risk_label) items.push(`risk: ${profile.risk_label}`);
  return items;
}
