import { Field, Input, Select, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { DatabaseAccountFields } from "../_shared/database-account-fields";
import { CredentialProfileLabelFields } from "../_shared/credential-profile-fields";
import { NetworkEndpointFields } from "../_shared/network-transport-fields";
import type { DatabaseConnectionFormProps } from "../_shared/database-form-types";
import type { emptyForm } from "./model";

export function SQLServerConnectorFormTemplate({
  form,
  mode = "create",
  onChange,
  passwordRequired = false,
}: DatabaseConnectionFormProps<ReturnType<typeof emptyForm>> & { passwordRequired?: boolean }) {
  return (
    <>
      <Notice tone="warn">
        Use a dedicated read-only SQL login, not sa or db_owner. Windows integrated authentication is not supported.
      </Notice>
      <Field>
        Connector name
        <Input value={form.name} onChange={(event) => onChange("name", event.target.value)} required />
      </Field>
      <NetworkEndpointFields form={form} onChange={onChange} hostLabel="SQL Server host" portLabel="TCP port" />
      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_200px]">
        <Field>
          Database
          <Input value={form.database} onChange={(event) => onChange("database", event.target.value)} required />
        </Field>
        <Field>
          TLS mode
          <Select value={form.tls_mode} onChange={(event) => onChange("tls_mode", event.target.value)}>
            <option value="verify_full">Verify full</option>
            <option value="disable">Disable (unencrypted)</option>
          </Select>
        </Field>
      </div>
      {form.tls_mode === "verify_full" ? (
        <>
          <Field>
            TLS server name
            <Input
              value={form.tls_server_name}
              onChange={(event) => onChange("tls_server_name", event.target.value)}
              placeholder="Optional certificate hostname"
            />
          </Field>
          <Field>
            Trusted CA certificate
            <Textarea
              value={form.ca_certificate}
              onChange={(event) => onChange("ca_certificate", event.target.value)}
              rows={3}
              placeholder="Optional public PEM CA certificate"
            />
          </Field>
        </>
      ) : (
        <Notice tone="bad">TLS is disabled. Queries, results and credentials may be exposed on the network.</Notice>
      )}
      <CredentialProfileLabelFields form={form} onChange={onChange} />
      <DatabaseAccountFields form={form} editing={mode === "edit"} passwordRequired={passwordRequired} onChange={onChange} />
    </>
  );
}
