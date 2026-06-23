import { Field, Input, Select, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";

export function DockerConnectorFormTemplate({ form, targets = [], onChange }) {
  const sshProfiles = sshProfileOptions(targets);
  return (
    <>
      <Notice tone="good">
        Docker actions run through bounded command templates over an SSH connector profile. Start lifecycle actions in Prompt mode until the workflow is trusted.
      </Notice>
      <Field>
        Connector name
        <Input value={form.name} onChange={(event) => onChange("name", event.target.value)} required />
      </Field>
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
      <Field>
        Docker command
        <Input value={form.docker_command} onChange={(event) => onChange("docker_command", event.target.value)} required />
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
        Container scope
        <Select value={form.scope_mode} onChange={(event) => onChange("scope_mode", event.target.value)}>
          <option value="all">All containers</option>
          <option value="selected">Selected containers</option>
        </Select>
      </Field>
      {form.scope_mode === "selected" ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            Allowed containers
            <Textarea rows={5} value={form.allowed_containers} onChange={(event) => onChange("allowed_containers", event.target.value)} placeholder={"api\nweb\nf6fc4f91e616"} />
          </Field>
          <Field>
            Allowed name patterns
            <Textarea rows={5} value={form.allowed_patterns} onChange={(event) => onChange("allowed_patterns", event.target.value)} placeholder={"project-api-*\n*_worker"} />
          </Field>
        </div>
      ) : null}
      <Notice>
        For one-container AI access, choose Selected containers and enter exactly that container name or ID. List/logs/restart actions are enforced against this profile scope.
      </Notice>
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
