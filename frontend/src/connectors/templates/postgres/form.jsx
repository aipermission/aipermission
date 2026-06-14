import { Field, Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";

export function PostgresConnectorFormTemplate({ form, mode = "create", onChange }) {
  const editing = mode === "edit";
  return (
    <>
      <Notice tone="good">
        The first Postgres credential profile is stored encrypted. Use a dedicated read-only database role for AI access.
      </Notice>
      <Field>
        Connector name
        <Input value={form.name} onChange={(event) => onChange("name", event.target.value)} required />
      </Field>
      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_120px]">
        <Field>
          Host
          <Input value={form.host} onChange={(event) => onChange("host", event.target.value)} required />
        </Field>
        <Field>
          Port
          <Input type="number" min="1" max="65535" value={form.port} onChange={(event) => onChange("port", event.target.value)} required />
        </Field>
      </div>
      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_160px]">
        <Field>
          Database
          <Input value={form.database} onChange={(event) => onChange("database", event.target.value)} required />
        </Field>
        <Field>
          SSL mode
          <Select value={form.ssl_mode} onChange={(event) => onChange("ssl_mode", event.target.value)}>
            <option value="require">Require</option>
            <option value="verify_full">Verify full</option>
            <option value="prefer">Prefer</option>
            <option value="disable">Disable</option>
          </Select>
        </Field>
      </div>
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
        Username
        <Input value={form.username} onChange={(event) => onChange("username", event.target.value)} autoComplete="off" required />
      </Field>
      <Field>
        Password
        <Input
          type="password"
          value={form.password}
          onChange={(event) => onChange("password", event.target.value)}
          autoComplete="new-password"
          placeholder={editing ? "Leave blank to keep the current encrypted password" : ""}
          required={!editing}
        />
      </Field>
    </>
  );
}
