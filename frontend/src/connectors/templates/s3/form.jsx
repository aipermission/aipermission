import { Checkbox, Field, Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { ConnectionModeFields } from "../_shared/network-transport-fields";
import { HostPingButton } from "../host-ping-button";

export function S3ConnectorFormTemplate({ form, mode = "create", targets = [], onChange }) {
  const editing = mode === "edit";
  return (
    <>
      <Notice tone="good">
        S3 supports S3-compatible providers such as AWS S3 and MinIO. Object downloads/uploads are bounded connector actions; use Prompt
        permissions before allowing writes.
      </Notice>
      <Field>
        Connector name
        <Input value={form.name} onChange={(event) => onChange("name", event.target.value)} required />
      </Field>
      <ConnectionModeFields
        form={form}
        targets={targets}
        onChange={onChange}
        overSSHNotice="Host and port are resolved from the SSH server. Use 127.0.0.1 when a MinIO/S3-compatible endpoint is only reachable from that server."
        directNotice="For MinIO or S3-compatible storage running on the same Linux host as AIPermission Docker, use host.docker.internal instead of localhost."
      />
      <div className="grid gap-3 sm:grid-cols-[120px_minmax(0,1fr)_120px]">
        <Field>
          Scheme
          <Select value={form.scheme || "https"} onChange={(event) => onChange("scheme", event.target.value)}>
            <option value="https">HTTPS</option>
            <option value="http">HTTP</option>
          </Select>
        </Field>
        <Field>
          <span className="flex items-center justify-between gap-2">
            <span>Endpoint host</span>
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
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field>
          Region
          <Input value={form.region} onChange={(event) => onChange("region", event.target.value)} placeholder="us-east-1" required />
        </Field>
        <Field>
          Bucket
          <Input value={form.bucket} onChange={(event) => onChange("bucket", event.target.value)} required />
        </Field>
      </div>
      <label className="flex items-start gap-3 rounded-md border border-stone-200 bg-stone-50 p-3 text-sm text-stone-700 dark-notice-neutral">
        <Checkbox checked={form.path_style !== false} onChange={(event) => onChange("path_style", event.target.checked)} />
        <span>
          <span className="block font-semibold">Path-style addressing</span>
          <span className="text-xs">Use /bucket/key URLs. Keep this enabled for most S3-compatible providers such as MinIO.</span>
        </span>
      </label>
      <label className="flex items-start gap-3 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-950 dark-notice-warn">
        <Checkbox
          checked={form.trust_conditional_requests === true}
          onChange={(event) => onChange("trust_conditional_requests", event.target.checked)}
        />
        <span>
          <span className="block font-semibold">Verified conditional requests</span>
          <span className="text-xs">
            Enable only after this provider's destination If-Match and If-None-Match behavior has been verified. Guarded mutations fail
            closed while disabled; approval requirements do not change.
          </span>
        </span>
      </label>
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
        Access key ID
        <Input value={form.access_key_id} onChange={(event) => onChange("access_key_id", event.target.value)} autoComplete="off" required />
      </Field>
      <Field>
        Secret access key
        <Input
          type="password"
          value={form.secret_access_key}
          onChange={(event) => onChange("secret_access_key", event.target.value)}
          autoComplete="new-password"
          required={!editing}
          placeholder={editing ? "Leave blank to keep the current encrypted secret" : "S3 secret access key"}
        />
      </Field>
      <Field>
        Session token
        <Input
          type="password"
          value={form.session_token}
          onChange={(event) => onChange("session_token", event.target.value)}
          autoComplete="new-password"
          placeholder={editing ? "Leave blank to keep the current session token" : "Optional temporary session token"}
        />
      </Field>
    </>
  );
}
