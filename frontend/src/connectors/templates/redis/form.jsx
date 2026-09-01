import { Field, Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { ConnectionModeFields } from "../_shared/network-transport-fields";
import { HostPingButton } from "../host-ping-button";
import { serverProductLabel } from "./model";

export function RedisConnectorFormTemplate({ form, mode = "create", targets = [], onChange }) {
  const editing = mode === "edit";
  const product = serverProductLabel(form);
  return (
    <>
      <Notice tone="good">
        {product} keys can contain secrets. Start with Prompt permissions for write and delete actions until the workflow is trusted.
      </Notice>
      <Field>
        Connector name
        <Input value={form.name} onChange={(event) => onChange("name", event.target.value)} required />
      </Field>
      <Field>
        Server product
        <Select value={form.server_family || "redis"} onChange={(event) => onChange("server_family", event.target.value)}>
          <option value="redis">Redis</option>
          <option value="valkey">Valkey</option>
        </Select>
      </Field>
      <ConnectionModeFields
        form={form}
        targets={targets}
        onChange={onChange}
        overSSHNotice={`Host and port are resolved from the SSH server. Use 127.0.0.1:6379 when ${product} only listens on the remote machine.`}
        directNotice={`For ${product} running on the same Linux host as AIPermission Docker, use host.docker.internal instead of localhost.`}
      />
      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_120px_120px]">
        <Field>
          <span className="flex items-center justify-between gap-2">
            <span>{product} host</span>
            <HostPingButton
              host={form.host}
              port={form.port}
              mode={form.connection_mode}
              transportTargetRef={form.transport_target_ref}
              projectID={form.project_id}
            />
          </span>
          <Input value={form.host} onChange={(event) => onChange("host", event.target.value)} required />
        </Field>
        <Field>
          Port
          <Input type="number" min="1" max="65535" value={form.port} onChange={(event) => onChange("port", event.target.value)} required />
        </Field>
        <Field>
          Database
          <Input type="number" min="0" max="1023" value={form.database} onChange={(event) => onChange("database", event.target.value)} />
        </Field>
      </div>
      <Field>
        TLS mode
        <Select value={form.tls_mode || "disable"} onChange={(event) => onChange("tls_mode", event.target.value)}>
          <option value="auto">Auto</option>
          <option value="disable">Disable</option>
          <option value="verify_full">Verify full</option>
        </Select>
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
          placeholder={`optional ${product} ACL username`}
        />
      </Field>
      <Field>
        Password
        <Input
          type="password"
          value={form.password}
          onChange={(event) => onChange("password", event.target.value)}
          autoComplete="new-password"
          placeholder={editing ? "Leave blank to keep the current encrypted password" : `optional ${product} password`}
        />
      </Field>
    </>
  );
}
