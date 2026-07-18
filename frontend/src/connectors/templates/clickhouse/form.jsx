import { Field, Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { NetworkTransportFields } from "../_shared/network-transport-fields";

export function ClickHouseConnectorFormTemplate({ form, mode = "create", targets = [], onChange }) {
  const editing = mode === "edit";
  return (
    <>
      <Notice tone="good">
        Use a dedicated read-only ClickHouse user. Query validation and execution limits are defense in depth, not a replacement for database permissions.
      </Notice>
      <Field>
        Connector name
        <Input value={form.name} onChange={(event) => onChange("name", event.target.value)} required />
      </Field>
      <NetworkTransportFields
        form={form}
        targets={targets}
        onChange={onChange}
        hostLabel="ClickHouse host"
        portLabel="Native port"
        overSSHNotice="Host and port are resolved from the SSH server. Use 127.0.0.1:9000 when ClickHouse only listens on the remote machine."
        directNotice="For ClickHouse running on the same Linux host as AIPermission Docker, use host.docker.internal instead of localhost."
      />
      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_160px]">
        <Field>
          Default database
          <Input value={form.database} onChange={(event) => onChange("database", event.target.value)} required />
        </Field>
        <Field>
          TLS mode
          <Select value={form.tls_mode} onChange={(event) => onChange("tls_mode", event.target.value)}>
            <option value="disable">Disable</option>
            <option value="verify_full">Verify full</option>
          </Select>
        </Field>
      </div>
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
        <Input value={form.username} onChange={(event) => onChange("username", event.target.value)} autoComplete="off" required />
      </Field>
      <Field>
        Password
        <Input
          type="password"
          value={form.password}
          onChange={(event) => onChange("password", event.target.value)}
          autoComplete="new-password"
          placeholder={editing ? "Leave blank to keep the current encrypted password" : "optional ClickHouse password"}
        />
      </Field>
    </>
  );
}
