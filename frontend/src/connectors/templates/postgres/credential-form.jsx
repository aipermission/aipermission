import { Database, Plus } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Field, Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";

export function PostgresCredentialFormTemplate({ targets, form, formMode = "create", state, onChange, onSubmit }) {
  const postgresTargets = targets.filter((target) => target.connector_kind === "postgres");
  const editing = formMode === "edit";
  return (
    <form className="grid gap-4" onSubmit={onSubmit}>
      {postgresTargets.length === 0 ? (
        <Notice tone="warn">Create a Postgres connector target before adding a Postgres credential profile.</Notice>
      ) : (
        <Notice tone="good">
          {editing
            ? "Update public credential metadata, or enter a new password to rotate the stored secret."
            : "Create a dedicated Postgres profile, then bind tokens to this profile from Console or Tokens."}
        </Notice>
      )}
      <Field>
        Connector target
        <Select value={form.target_id} onChange={(event) => onChange({ ...form, target_id: event.target.value })} disabled={editing} required>
          <option value="" disabled>
            Select Postgres target
          </option>
          {postgresTargets.map((target) => (
            <option value={target.id} key={target.id}>
              {target.name} · {target.config?.host}:{target.config?.port}/{target.config?.database}
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
        Username
        <Input value={form.username} onChange={(event) => onChange({ ...form, username: event.target.value })} autoComplete="off" required />
      </Field>
      <Field>
        {editing ? "New password" : "Password"}
        <Input
          type="password"
          value={form.password}
          onChange={(event) => onChange({ ...form, password: event.target.value })}
          autoComplete="new-password"
          placeholder={editing ? "Leave blank to keep current password" : ""}
          required={!editing}
        />
      </Field>
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      <Button type="submit" disabled={state.state === "saving" || postgresTargets.length === 0}>
        <Plus className="h-4 w-4" />
        {state.state === "saving" ? "Saving..." : editing ? "Save Postgres credential" : "Create Postgres credential"}
      </Button>
      <Notice>
        <Database className="mr-2 inline h-4 w-4" />
        The password is stored in the encrypted local database and is never returned to MCP or REST list responses.
      </Notice>
    </form>
  );
}
