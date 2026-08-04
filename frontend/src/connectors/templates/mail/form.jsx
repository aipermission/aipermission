import { Field, Input, Select, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { HostPingButton } from "../host-ping-button";
import { sshProfileOptions } from "../_shared/network-transport-fields";
import { MailCredentialFields } from "./profile-fields";

export function MailConnectorFormTemplate({ form, mode = "create", targets = [], onChange }) {
  const overSSH = form.connection_mode === "over_ssh";
  return (
    <>
      <Notice tone="good">Mail reads use IMAP without changing read state. Sending uses SMTP and remains a separate write action.</Notice>
      <Field>
        Connector name
        <Input value={form.name} onChange={(event) => onChange("name", event.target.value)} required />
      </Field>
      <Field>
        Connection mode
        <Select value={form.connection_mode} onChange={(event) => onChange("connection_mode", event.target.value)}>
          <option value="direct">Direct from this gateway</option>
          <option value="over_ssh">Over an SSH connector profile</option>
        </Select>
      </Field>
      {overSSH ? (
        <Field>
          SSH transport profile
          <Select value={form.transport_target_ref} onChange={(event) => onChange("transport_target_ref", event.target.value)} required>
            <option value="" disabled>
              Select SSH profile
            </option>
            {sshProfileOptions(targets).map((profile) => (
              <option value={profile.ref} key={profile.ref}>
                {profile.label}
              </option>
            ))}
          </Select>
        </Field>
      ) : null}
      <Notice>
        {overSSH
          ? "IMAP and SMTP hostnames resolve from the selected SSH server. The transport profile must belong to this project."
          : "The local gateway connects to both mail endpoints. Use host.docker.internal only for services on the Docker host."}
      </Notice>
      <MailEndpointFields prefix="imap" label="IMAP" form={form} onChange={onChange} />
      <MailEndpointFields prefix="smtp" label="SMTP" form={form} onChange={onChange} />
      <Field>
        Allowed recipient domains
        <Textarea
          className="min-h-20 font-mono text-xs"
          value={form.allowed_recipient_domains}
          onChange={(event) => onChange("allowed_recipient_domains", event.target.value)}
          placeholder={"example.com\ncustomer.example"}
        />
        <span className="text-xs font-normal text-stone-500">
          Optional exact domains, one per line. Their subdomains are also accepted.
        </span>
      </Field>
      <MailCredentialFields form={form} editing={mode === "edit"} onChange={(field, value) => onChange(field, value)} />
    </>
  );
}

function MailEndpointFields({ prefix, label, form, onChange }) {
  const hostField = `${prefix}_host`;
  const portField = `${prefix}_port`;
  const tlsField = `${prefix}_tls_mode`;
  return (
    <fieldset className="grid gap-3 rounded-lg border border-stone-200 p-3">
      <legend className="px-1 text-sm font-semibold text-stone-700">{label} endpoint</legend>
      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_110px_150px]">
        <Field>
          <span className="flex items-center justify-between gap-2">
            <span>{label} host</span>
            <HostPingButton
              host={form[hostField]}
              port={form[portField]}
              mode={form.connection_mode}
              transportTargetRef={form.transport_target_ref}
              projectID={form.project_id}
            />
          </span>
          <Input value={form[hostField]} onChange={(event) => onChange(hostField, event.target.value)} required />
        </Field>
        <Field>
          Port
          <Input
            type="number"
            min="1"
            max="65535"
            value={form[portField]}
            onChange={(event) => onChange(portField, event.target.value)}
            required
          />
        </Field>
        <Field>
          TLS mode
          <Select value={form[tlsField]} onChange={(event) => onChange(tlsField, event.target.value)}>
            <option value="implicit_tls">SSL/TLS (implicit TLS)</option>
            <option value="starttls">STARTTLS</option>
          </Select>
        </Field>
      </div>
    </fieldset>
  );
}
