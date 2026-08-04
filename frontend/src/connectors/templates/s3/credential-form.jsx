import { Database, Plus } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Field, Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";

export function S3CredentialFormTemplate({ targets, form, formMode = "create", state, onChange, onSubmit }) {
  const s3Targets = targets.filter((target) => target.connector_kind === "s3");
  const editing = formMode === "edit";
  return (
    <form className="grid gap-4" onSubmit={onSubmit}>
      {s3Targets.length === 0 ? (
        <Notice tone="warn">Create an S3 connector target before adding an S3 credential profile.</Notice>
      ) : (
        <Notice tone="good">
          {editing
            ? "Update public S3 profile metadata, or enter a new secret to rotate the stored key."
            : "Create an S3 profile, then bind tokens to this profile from Console or Tokens."}
        </Notice>
      )}
      <Field>
        Connector target
        <Select
          value={form.target_id}
          onChange={(event) => onChange({ ...form, target_id: event.target.value })}
          disabled={editing}
          required
        >
          <option value="" disabled>
            Select S3 target
          </option>
          {s3Targets.map((target) => (
            <option value={target.id} key={target.id}>
              {target.name} · {target.config?.scheme || "https"}://{target.config?.host}:{target.config?.port || 443}/
              {target.config?.bucket || "bucket"}
            </option>
          ))}
        </Select>
      </Field>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field>
          Profile label
          <Input value={form.profile_label} onChange={(event) => onChange({ ...form, profile_label: event.target.value })} required />
        </Field>
        <Field>
          Risk label
          <Input value={form.risk_label} onChange={(event) => onChange({ ...form, risk_label: event.target.value })} />
        </Field>
      </div>
      <Field>
        Access key ID
        <Input
          value={form.access_key_id}
          onChange={(event) => onChange({ ...form, access_key_id: event.target.value })}
          autoComplete="off"
          required
        />
      </Field>
      <Field>
        {editing ? "New secret access key" : "Secret access key"}
        <Input
          type="password"
          value={form.secret_access_key}
          onChange={(event) => onChange({ ...form, secret_access_key: event.target.value })}
          autoComplete="new-password"
          required={!editing}
          placeholder={editing ? "Leave blank to keep current secret" : "S3 secret access key"}
        />
      </Field>
      <Field>
        {editing ? "New session token" : "Session token"}
        <Input
          type="password"
          value={form.session_token}
          onChange={(event) => onChange({ ...form, session_token: event.target.value })}
          autoComplete="new-password"
          placeholder={editing ? "Leave blank to keep current token" : "Optional temporary session token"}
        />
      </Field>
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      <Button type="submit" disabled={state.state === "saving" || s3Targets.length === 0}>
        <Plus className="h-4 w-4" />
        {state.state === "saving" ? "Saving..." : editing ? "Save S3 credential" : "Create S3 credential"}
      </Button>
      <Notice>
        <Database className="mr-2 inline h-4 w-4" />
        S3 secrets are stored in the encrypted local database and are never returned to MCP or REST list responses.
      </Notice>
    </form>
  );
}
