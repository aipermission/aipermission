import { Database, Plus } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Field, Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";

export function RabbitMQCredentialFormTemplate({ targets, form, formMode = "create", state, onChange, onSubmit }) {
  const rabbitTargets = targets.filter((target) => target.connector_kind === "rabbitmq");
  const editing = formMode === "edit";
  return (
    <form className="grid gap-4" onSubmit={onSubmit}>
      {rabbitTargets.length === 0 ? (
        <Notice tone="warn">Create a RabbitMQ connector target before adding a RabbitMQ credential profile.</Notice>
      ) : (
        <Notice tone="good">
          {editing ? "Update public RabbitMQ profile metadata, or enter a new password to rotate the stored secret." : "Create a RabbitMQ profile, then bind tokens to this profile from Console or Tokens."}
        </Notice>
      )}
      <Field>
        Connector target
        <Select value={form.target_id} onChange={(event) => onChange({ ...form, target_id: event.target.value })} disabled={editing} required>
          <option value="" disabled>
            Select RabbitMQ target
          </option>
          {rabbitTargets.map((target) => (
            <option value={target.id} key={target.id}>
          {target.name} · management {target.config?.scheme || "http"}://{target.config?.host}:{target.config?.port || 15672} · {target.config?.vhost || "/"}
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
        <Input value={form.username} onChange={(event) => onChange({ ...form, username: event.target.value })} autoComplete="off" placeholder="RabbitMQ Management API username" required />
      </Field>
      <Field>
        {editing ? "New password" : "Password"}
        <Input
          type="password"
          value={form.password}
          onChange={(event) => onChange({ ...form, password: event.target.value })}
          autoComplete="new-password"
          required={!editing}
          placeholder={editing ? "Leave blank to keep current password" : "RabbitMQ Management API password"}
        />
      </Field>
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      <Button type="submit" disabled={state.state === "saving" || rabbitTargets.length === 0}>
        <Plus className="h-4 w-4" />
        {state.state === "saving" ? "Saving..." : editing ? "Save RabbitMQ credential" : "Create RabbitMQ credential"}
      </Button>
      <Notice>
        <Database className="mr-2 inline h-4 w-4" />
        RabbitMQ secrets are stored in the encrypted local database and are never returned to MCP or REST list responses.
      </Notice>
    </form>
  );
}
