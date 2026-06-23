import { Field, Input, Select, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";

export function KubernetesConnectorFormTemplate({ form, targets = [], onChange }) {
  const sshProfiles = sshProfileOptions(targets);
  return (
    <>
      <Notice tone="good">
        Kubernetes uses bounded kubectl templates over an SSH connector profile. Start with read-only actions and keep rollout restart in Prompt mode.
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
      <div className="grid gap-3 sm:grid-cols-3">
        <Field>
          kubectl command
          <Input value={form.kubectl_command} onChange={(event) => onChange("kubectl_command", event.target.value)} required />
        </Field>
        <Field>
          Context
          <Input value={form.context} onChange={(event) => onChange("context", event.target.value)} placeholder="optional" />
        </Field>
        <Field>
          Default namespace
          <Input value={form.default_namespace} onChange={(event) => onChange("default_namespace", event.target.value)} placeholder="optional" />
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
        Namespace scope
        <Select value={form.scope_mode} onChange={(event) => onChange("scope_mode", event.target.value)}>
          <option value="all">All namespaces</option>
          <option value="selected">Selected namespaces</option>
        </Select>
      </Field>
      {form.scope_mode === "selected" ? (
        <Field>
          Namespaces
          <Textarea rows={5} value={form.namespaces} onChange={(event) => onChange("namespaces", event.target.value)} placeholder={"production\nmonitoring"} />
        </Field>
      ) : null}
      <Notice>
        AIPermission does not import kubeconfig or service-account tokens in this MVP. The selected SSH profile must reach a host where kubectl is already configured.
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
