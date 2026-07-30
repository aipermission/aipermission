import { Database, Plus } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Field, Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { connectorProductLabel, serverProductLabel } from "./model";

export function RedisCredentialFormTemplate({ targets, form, formMode = "create", state, onChange, onSubmit }) {
  const redisTargets = targets.filter((target) => target.connector_kind === "redis");
  const selectedTarget = redisTargets.find((target) => Number(target.id) === Number(form.target_id));
  const product = selectedTarget ? serverProductLabel(selectedTarget) : connectorProductLabel;
  const editing = formMode === "edit";
  return (
    <form className="grid gap-4" onSubmit={onSubmit}>
      {redisTargets.length === 0 ? (
        <Notice tone="warn">Create a {connectorProductLabel} connector target before adding a credential profile.</Notice>
      ) : (
        <Notice tone="good">
          {editing ? `Update public ${product} profile metadata, or enter a new password to rotate the stored secret.` : `Create a ${product} profile, then bind tokens to this profile from Console or Tokens.`}
        </Notice>
      )}
      <Field>
        Connector target
        <Select value={form.target_id} onChange={(event) => onChange({ ...form, target_id: event.target.value })} disabled={editing} required>
          <option value="" disabled>
            Select {connectorProductLabel} target
          </option>
          {redisTargets.map((target) => (
            <option value={target.id} key={target.id}>
              {target.name} · {serverProductLabel(target)} · {target.config?.host}:{target.config?.port}/{target.config?.database || 0}
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
        <Input value={form.username} onChange={(event) => onChange({ ...form, username: event.target.value })} autoComplete="off" placeholder={`optional ${product} ACL username`} />
      </Field>
      <Field>
        {editing ? "New password" : "Password"}
        <Input
          type="password"
          value={form.password}
          onChange={(event) => onChange({ ...form, password: event.target.value })}
          autoComplete="new-password"
          placeholder={editing ? "Leave blank to keep current password" : `optional ${product} password`}
        />
      </Field>
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      <Button type="submit" disabled={state.state === "saving" || redisTargets.length === 0}>
        <Plus className="h-4 w-4" />
        {state.state === "saving" ? "Saving..." : editing ? `Save ${product} credential` : `Create ${product} credential`}
      </Button>
      <Notice>
        <Database className="mr-2 inline h-4 w-4" />
        Redis and Valkey secrets are stored in the encrypted local database and are never returned to MCP or REST list responses.
      </Notice>
    </form>
  );
}
