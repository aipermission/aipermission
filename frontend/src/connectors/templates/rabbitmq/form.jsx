import { Field, Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { HostPingButton } from "../host-ping-button";

export function RabbitMQConnectorFormTemplate({ form, mode = "create", targets = [], onChange }) {
  const editing = mode === "edit";
  const sshProfiles = sshProfileOptions(targets);
  const overSSH = form.connection_mode === "over_ssh";
  return (
    <>
      <Notice tone="good">
        RabbitMQ uses the Management API, not the AMQP listener. Use the port from the management URL, usually 15672, and start with Prompt permissions for message peeking.
      </Notice>
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
            {sshProfiles.map((profile) => (
              <option value={profile.ref} key={profile.ref}>
                {profile.label}
              </option>
            ))}
          </Select>
        </Field>
      ) : null}
      {overSSH ? (
        <Notice>
          Host and port are resolved from the SSH server. Use 127.0.0.1:15672 when RabbitMQ Management only listens on the remote machine; do not use the AMQP port.
        </Notice>
      ) : (
        <Notice>
          For RabbitMQ Management running on the same Linux host as AIPermission Docker, use host.docker.internal instead of localhost.
        </Notice>
      )}
      <div className="grid gap-3 sm:grid-cols-[120px_minmax(0,1fr)_120px]">
        <Field>
          Scheme
          <Select value={form.scheme || "http"} onChange={(event) => onChange("scheme", event.target.value)}>
            <option value="http">HTTP</option>
            <option value="https">HTTPS</option>
          </Select>
        </Field>
        <Field>
          <span className="flex items-center justify-between gap-2">
            <span>Management host</span>
            <HostPingButton host={form.host} port={form.port} mode={form.connection_mode} transportTargetRef={form.transport_target_ref} />
          </span>
          <Input value={form.host} onChange={(event) => onChange("host", event.target.value)} required />
        </Field>
        <Field>
          Management API port
          <Input type="number" min="1" max="65535" value={form.port} onChange={(event) => onChange("port", event.target.value)} placeholder="15672" required />
        </Field>
      </div>
      <Field>
        Default vhost
        <Input value={form.vhost} onChange={(event) => onChange("vhost", event.target.value)} placeholder="/" />
      </Field>
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
      <Field>
        Username
        <Input value={form.username} onChange={(event) => onChange("username", event.target.value)} autoComplete="off" placeholder="RabbitMQ Management API username" required />
      </Field>
      <Field>
        Password
        <Input
          type="password"
          value={form.password}
          onChange={(event) => onChange("password", event.target.value)}
          autoComplete="new-password"
          required={!editing}
          placeholder={editing ? "Leave blank to keep the current encrypted password" : "RabbitMQ Management API password"}
        />
      </Field>
    </>
  );
}

function sshProfileOptions(targets) {
  return (targets || [])
    .filter((target) => target.connector_kind === "ssh")
    .flatMap((target) =>
      (target.profiles || []).map((profile) => ({
        ref: profile.ref || `${target.connector_kind}:${target.id}:${profile.id}`,
        label: `${target.name} / ${profile.label} · ${target.config?.host || "host"}:${target.config?.port || 22}`,
      }))
    );
}
