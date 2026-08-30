import { Container, Plus } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Field, Select, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { CredentialProfileFields } from "../_shared/credential-profile-fields";

export function DockerCredentialFormTemplate({ targets, form, formMode = "create", state, onChange, onSubmit }) {
  const dockerTargets = targets.filter((target) => target.connector_kind === "docker");
  const editing = formMode === "edit";
  return (
    <form className="grid gap-4" onSubmit={onSubmit}>
      {dockerTargets.length === 0 ? (
        <Notice tone="warn">Create a Docker connector target before adding a Docker credential scope.</Notice>
      ) : (
        <Notice tone="good">
          {editing
            ? "Update this Docker scope. Token permissions bound to this profile will use the new container scope immediately."
            : "Create a Docker scope, then bind tokens to this profile from Console or Tokens."}
        </Notice>
      )}
      <CredentialProfileFields
        targets={dockerTargets}
        form={form}
        editing={editing}
        onChange={onChange}
        targetPlaceholder="Select Docker target"
        targetOptionLabel={(target) => `${target.name} · ${target.config?.transport_target_ref || "no transport"}`}
      />
      <Field>
        Container scope
        <Select value={form.scope_mode} onChange={(event) => onChange({ ...form, scope_mode: event.target.value })}>
          <option value="all">All containers</option>
          <option value="selected">Selected containers</option>
        </Select>
      </Field>
      {form.scope_mode === "selected" ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            Allowed containers
            <Textarea
              rows={6}
              value={form.allowed_containers}
              onChange={(event) => onChange({ ...form, allowed_containers: event.target.value })}
              placeholder={"api\nweb\ncontainer-id-prefix"}
            />
          </Field>
          <Field>
            Allowed name patterns
            <Textarea
              rows={6}
              value={form.allowed_patterns}
              onChange={(event) => onChange({ ...form, allowed_patterns: event.target.value })}
              placeholder={"project-api-*\n*_worker"}
            />
          </Field>
        </div>
      ) : null}
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      <Button type="submit" disabled={state.state === "saving" || dockerTargets.length === 0}>
        <Plus className="h-4 w-4" />
        {state.state === "saving" ? "Saving..." : editing ? "Save Docker scope" : "Create Docker scope"}
      </Button>
      <Notice>
        <Container className="mr-2 inline h-4 w-4" />
        Docker scopes contain public container allowlists only. Lifecycle writes still require token action permission and approval policy.
      </Notice>
    </form>
  );
}
