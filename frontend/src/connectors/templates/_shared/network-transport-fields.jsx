import { Field, Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { HostPingButton } from "../host-ping-button";

export function NetworkTransportFields({
  form,
  targets = [],
  onChange,
  hostLabel = "Host",
  portLabel = "Port",
  overSSHNotice,
  directNotice,
}) {
  const overSSH = form.connection_mode === "over_ssh";
  const sshProfiles = sshProfileOptions(targets);
  return (
    <>
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
      {overSSH ? <Notice>{overSSHNotice}</Notice> : <Notice>{directNotice}</Notice>}
      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_120px]">
        <Field>
          <span className="flex items-center justify-between gap-2">
            <span>{hostLabel}</span>
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
          {portLabel}
          <Input type="number" min="1" max="65535" value={form.port} onChange={(event) => onChange("port", event.target.value)} required />
        </Field>
      </div>
    </>
  );
}

export function sshProfileOptions(targets) {
  return (targets || [])
    .filter((target) => target.connector_kind === "ssh")
    .flatMap((target) =>
      (target.profiles || []).map((profile) => ({
        ref: profile.ref || `${target.connector_kind}:${target.id}:${profile.id}`,
        label: `${target.name} / ${profile.label} · ${target.config?.host || "host"}:${target.config?.port || 22}`,
      })),
    );
}
