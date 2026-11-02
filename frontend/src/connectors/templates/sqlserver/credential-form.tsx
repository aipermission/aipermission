import { Save } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Notice } from "../../../components/ui/notice";
import { DatabaseAccountFields } from "../_shared/database-account-fields";
import { CredentialProfileFields } from "../_shared/credential-profile-fields";
import type { DatabaseCredentialFormProps } from "../_shared/database-form-types";

export function SQLServerCredentialFormTemplate({
  targets,
  form,
  formMode = "create",
  state,
  onChange,
  onSubmit,
  passwordRequired = false,
}: DatabaseCredentialFormProps & { passwordRequired?: boolean }) {
  const available = targets.filter((target) => target.connector_kind === "sqlserver");
  const editing = formMode === "edit";
  return (
    <form className="grid gap-4" onSubmit={onSubmit}>
      <CredentialProfileFields
        targets={available}
        form={form}
        editing={editing}
        onChange={onChange}
        targetPlaceholder="Select SQL Server target"
        targetOptionLabel={(target) => `${target.name} · ${target.config?.database}`}
      />
      <DatabaseAccountFields
        form={form}
        editing={editing}
        passwordRequired={passwordRequired}
        onChange={(name, value) => onChange({ ...form, [name]: value })}
      />
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      {available.length === 0 ? <Notice tone="warn">Create a SQL Server connector before adding a credential profile.</Notice> : null}
      <Button type="submit" disabled={state.state === "saving" || available.length === 0}>
        <Save className="h-4 w-4" />
        {state.state === "saving" ? "Saving..." : editing ? "Save SQL Server credential" : "Create SQL Server credential"}
      </Button>
    </form>
  );
}
