import { Database, Plus } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Field, Input } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { CredentialProfileFields } from "../_shared/credential-profile-fields";

export function ClickHouseCredentialFormTemplate({ targets, form, formMode = "create", state, onChange, onSubmit }) {
  const clickHouseTargets = targets.filter((target) => target.connector_kind === "clickhouse");
  const editing = formMode === "edit";
  return (
    <form className="grid gap-4" onSubmit={onSubmit}>
      {clickHouseTargets.length === 0 ? (
        <Notice tone="warn">Create a ClickHouse connector target before adding a ClickHouse credential profile.</Notice>
      ) : (
        <Notice tone="good">
          {editing
            ? "Update public credential metadata, or enter a new password to rotate the stored secret."
            : "Create a dedicated ClickHouse profile, then bind tokens to this profile from Console or Tokens."}
        </Notice>
      )}
      <CredentialProfileFields
        targets={clickHouseTargets}
        form={form}
        editing={editing}
        onChange={onChange}
        targetPlaceholder="Select ClickHouse target"
        targetOptionLabel={(target) => `${target.name} · ${target.config?.host}:${target.config?.port}/${target.config?.database}`}
      />
      <Field>
        Username
        <Input
          value={form.username}
          onChange={(event) => onChange({ ...form, username: event.target.value })}
          autoComplete="off"
          required
        />
      </Field>
      <Field>
        {editing ? "New password" : "Password"}
        <Input
          type="password"
          value={form.password}
          onChange={(event) => onChange({ ...form, password: event.target.value })}
          autoComplete="new-password"
          placeholder={editing ? "Leave blank to keep current password" : "optional ClickHouse password"}
        />
      </Field>
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      <Button type="submit" disabled={state.state === "saving" || clickHouseTargets.length === 0}>
        <Plus className="h-4 w-4" />
        {state.state === "saving" ? "Saving..." : editing ? "Save ClickHouse credential" : "Create ClickHouse credential"}
      </Button>
      <Notice>
        <Database className="mr-2 inline h-4 w-4" />
        The password is stored in the encrypted local database and is never returned to MCP or REST list responses.
      </Notice>
    </form>
  );
}
