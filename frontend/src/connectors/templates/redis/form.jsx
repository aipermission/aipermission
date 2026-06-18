import { Field, Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { HostPingButton } from "../host-ping-button";

export function RedisConnectorFormTemplate({ form, mode = "create", targets = [], onChange }) {
  const editing = mode === "edit";
  const sshProfiles = sshProfileOptions(targets);
  const overSSH = form.connection_mode === "over_ssh";
  return (
    <>
      <Notice tone="good">
        Redis keys can contain secrets. Start with Prompt permissions for write and delete actions until the workflow is trusted.
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
          Host and port are resolved from the SSH server. Use 127.0.0.1:6379 when Redis only listens on the remote machine.
        </Notice>
      ) : (
        <Notice>
          For Redis running on the same Linux host as AIPermission Docker, use host.docker.internal instead of localhost.
        </Notice>
      )}
      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_120px_120px]">
        <Field>
          <span className="flex items-center justify-between gap-2">
            <span>Redis host</span>
            <HostPingButton host={form.host} port={form.port} mode={form.connection_mode} transportTargetRef={form.transport_target_ref} />
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
        <Input value={form.username} onChange={(event) => onChange("username", event.target.value)} autoComplete="off" placeholder="optional Redis ACL username" />
      </Field>
      <Field>
        Password
        <Input
          type="password"
          value={form.password}
          onChange={(event) => onChange("password", event.target.value)}
          autoComplete="new-password"
          placeholder={editing ? "Leave blank to keep the current encrypted password" : "optional Redis password"}
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
