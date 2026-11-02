import { Field, Input } from "../../../components/ui/form";
import type { ConnectorFieldChange } from "./connector-form-types";

type Account = { username: string; password: string };

export function DatabaseAccountFields({
  form,
  editing,
  passwordRequired = false,
  onChange,
}: {
  form: Account;
  editing: boolean;
  passwordRequired?: boolean;
  onChange: ConnectorFieldChange<Account>;
}) {
  return (
    <>
      <Field>
        Username
        <Input value={form.username} onChange={(event) => onChange("username", event.target.value)} required autoComplete="off" />
      </Field>
      <Field>
        {editing ? "New password" : "Password"}
        <Input
          type="password"
          value={form.password}
          onChange={(event) => onChange("password", event.target.value)}
          required={!editing || passwordRequired}
          autoComplete="new-password"
          placeholder={passwordRequired ? "Required when changing the username" : editing ? "Leave blank to keep the current password" : ""}
        />
      </Field>
    </>
  );
}
