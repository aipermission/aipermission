import { Server, Plus } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Field, Select, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { CredentialProfileFields } from "../_shared/credential-profile-fields";

export function KubernetesCredentialFormTemplate({ targets, form, formMode = "create", state, onChange, onSubmit }) {
  const kubernetesTargets = targets.filter((target) => target.connector_kind === "kubernetes");
  const editing = formMode === "edit";
  return (
    <form className="grid gap-4" onSubmit={onSubmit}>
      {kubernetesTargets.length === 0 ? (
        <Notice tone="warn">Create a Kubernetes connector target before adding a namespace scope.</Notice>
      ) : (
        <Notice tone="good">
          {editing
            ? "Update this Kubernetes namespace scope. Token permissions bound to this profile will use the new scope immediately."
            : "Create a namespace scope, then bind tokens to this profile from Console or Tokens."}
        </Notice>
      )}
      <CredentialProfileFields
        targets={kubernetesTargets}
        form={form}
        editing={editing}
        onChange={onChange}
        targetPlaceholder="Select Kubernetes target"
        targetOptionLabel={(target) => `${target.name} · ${target.config?.transport_target_ref || "no transport"}`}
      />
      <Field>
        Namespace scope
        <Select value={form.scope_mode} onChange={(event) => onChange({ ...form, scope_mode: event.target.value })}>
          <option value="all">All namespaces</option>
          <option value="selected">Selected namespaces</option>
        </Select>
      </Field>
      {form.scope_mode === "selected" ? (
        <Field>
          Namespaces
          <Textarea
            rows={6}
            value={form.namespaces}
            onChange={(event) => onChange({ ...form, namespaces: event.target.value })}
            placeholder={"production\nmonitoring\nbackend"}
          />
        </Field>
      ) : null}
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      <Button type="submit" disabled={state.state === "saving" || kubernetesTargets.length === 0}>
        <Plus className="h-4 w-4" />
        {state.state === "saving" ? "Saving..." : editing ? "Save namespace scope" : "Create namespace scope"}
      </Button>
      <Notice>
        <Server className="mr-2 inline h-4 w-4" />
        Namespace scopes contain public allowlists only. Kubernetes auth still comes from the selected remote kubectl environment.
      </Notice>
    </form>
  );
}
