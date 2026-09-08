import { Field, Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { ConnectionModeFields, NetworkEndpointFields } from "../_shared/network-transport-fields";

export function RabbitMQConnectorFormTemplate({ form, mode = "create", targets = [], onChange }) {
  const editing = mode === "edit";
  return (
    <>
      <Notice tone="good">
        RabbitMQ uses the Management API, not the AMQP listener. Use the port from the management URL, usually 15672, and start with Prompt
        permissions for message peeking.
      </Notice>
      <Field>
        Connector name
        <Input value={form.name} onChange={(event) => onChange("name", event.target.value)} required />
      </Field>
      <ConnectionModeFields
        form={form}
        targets={targets}
        onChange={onChange}
        transportNotice="Host and port are resolved from the SSH server. Use 127.0.0.1:15672 when RabbitMQ Management only listens on the remote machine; do not use the AMQP port."
        directNotice="For RabbitMQ Management running on the same Linux host as AIPermission Docker, use host.docker.internal instead of localhost."
      />
      <NetworkEndpointFields
        form={form}
        onChange={onChange}
        hostLabel="Management host"
        portLabel="Management API port"
        portPlaceholder="15672"
        className="sm:grid-cols-[120px_minmax(0,1fr)_120px]"
        leading={
          <Field>
            Scheme
            <Select value={form.scheme || "http"} onChange={(event) => onChange("scheme", event.target.value)}>
              <option value="auto">Auto</option>
              <option value="http">HTTP</option>
              <option value="https">HTTPS</option>
            </Select>
          </Field>
        }
      />
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
        <Input
          value={form.username}
          onChange={(event) => onChange("username", event.target.value)}
          autoComplete="off"
          placeholder="RabbitMQ Management API username"
          required
        />
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
