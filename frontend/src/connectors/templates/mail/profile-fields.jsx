import { Checkbox, Field, Input, Select, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";

export function MailCredentialFields({ form, editing, onChange }) {
  const imapEnabled = form.imap_enabled !== false;
  const smtpMode = form.smtp_auth_mode || "disabled";
  return (
    <fieldset className="grid gap-4 rounded-lg border border-stone-200 p-3">
      <legend className="px-1 text-sm font-semibold text-stone-700">Mailbox profile</legend>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field>
          Profile label
          <Input value={form.profile_label} onChange={(event) => onChange("profile_label", event.target.value)} required />
        </Field>
        <Field>
          Risk label
          <Input value={form.risk_label} onChange={(event) => onChange("risk_label", event.target.value)} />
        </Field>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field>
          Mailbox address
          <Input
            type="email"
            value={form.mailbox_address}
            onChange={(event) => onChange("mailbox_address", event.target.value)}
            autoComplete="off"
            required
          />
        </Field>
        <Field>
          Display name
          <Input value={form.display_name} onChange={(event) => onChange("display_name", event.target.value)} />
        </Field>
      </div>
      <Field>
        Reply-to
        <Input type="email" value={form.reply_to} onChange={(event) => onChange("reply_to", event.target.value)} autoComplete="off" />
      </Field>
      <label className="flex items-center gap-2 text-sm font-medium text-stone-800">
        <Checkbox
          checked={imapEnabled}
          onChange={(event) => {
            const enabled = event.target.checked;
            onChange("imap_enabled", enabled);
            if (!enabled && smtpMode === "reuse_imap") onChange("smtp_auth_mode", "disabled");
          }}
        />
        Enable IMAP mailbox access
      </label>
      {imapEnabled ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            IMAP username
            <Input
              value={form.imap_username}
              onChange={(event) => onChange("imap_username", event.target.value)}
              autoComplete="off"
              required={!editing}
              placeholder={editing ? "Leave blank to keep current username" : "Mailbox username"}
            />
          </Field>
          <Field>
            IMAP password or app password
            <Input
              type="password"
              value={form.imap_password}
              onChange={(event) => onChange("imap_password", event.target.value)}
              autoComplete="new-password"
              required={!editing}
              placeholder={editing ? "Leave blank to keep current password" : "App password recommended"}
            />
          </Field>
        </div>
      ) : null}
      <Field>
        SMTP authentication
        <Select value={smtpMode} onChange={(event) => onChange("smtp_auth_mode", event.target.value)}>
          <option value="disabled">Disabled</option>
          <option value="reuse_imap" disabled={!imapEnabled}>
            Reuse IMAP credentials
          </option>
          <option value="separate">Separate credentials</option>
        </Select>
      </Field>
      {smtpMode === "separate" ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            SMTP username
            <Input
              value={form.smtp_username}
              onChange={(event) => onChange("smtp_username", event.target.value)}
              autoComplete="off"
              required={!editing}
              placeholder={editing ? "Leave blank to keep current username" : "SMTP username"}
            />
          </Field>
          <Field>
            SMTP password or app password
            <Input
              type="password"
              value={form.smtp_password}
              onChange={(event) => onChange("smtp_password", event.target.value)}
              autoComplete="new-password"
              required={!editing}
              placeholder={editing ? "Leave blank to keep current password" : "SMTP password"}
            />
          </Field>
        </div>
      ) : null}
      {!imapEnabled && smtpMode === "disabled" ? <Notice tone="bad">Enable IMAP or SMTP before saving this mailbox profile.</Notice> : null}
      {imapEnabled ? <FolderPolicyFields form={form} onChange={onChange} /> : null}
      <Notice tone="warn">
        Mailbox bodies and outbound drafts may be stored in encrypted local history and included in encrypted backups until retention
        cleanup.
      </Notice>
    </fieldset>
  );
}

function FolderPolicyFields({ form, onChange }) {
  return (
    <>
      <div className="grid gap-3 lg:grid-cols-3">
        <FolderListField
          label="Readable folders"
          value={form.allowed_read_folders}
          onChange={(value) => onChange("allowed_read_folders", value)}
        />
        <FolderListField
          label="Writable source folders"
          value={form.allowed_mutation_source_folders}
          onChange={(value) => onChange("allowed_mutation_source_folders", value)}
        />
        <FolderListField
          label="Move destinations"
          value={form.allowed_mutation_destination_folders}
          onChange={(value) => onChange("allowed_mutation_destination_folders", value)}
        />
      </div>
      <div className="grid gap-3 sm:grid-cols-3">
        <Field>
          Sent folder
          <Input value={form.sent_folder} onChange={(event) => onChange("sent_folder", event.target.value)} />
        </Field>
        <Field>
          Archive folder
          <Input value={form.archive_folder} onChange={(event) => onChange("archive_folder", event.target.value)} />
        </Field>
        <Field>
          Trash folder
          <Input value={form.trash_folder} onChange={(event) => onChange("trash_folder", event.target.value)} />
        </Field>
      </div>
    </>
  );
}

function FolderListField({ label, value, onChange }) {
  return (
    <Field>
      {label}
      <Textarea
        className="min-h-24 font-mono text-xs"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder="One folder per line"
      />
    </Field>
  );
}
