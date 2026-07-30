import { Field, Input, Select } from "../../../components/ui/form";

export function KafkaSASLFields({ form, editing = false, onChange }) {
  const secured = form.sasl_mechanism !== "none";
  const passwordRequired = secured && (!editing || (form.existing_sasl_mechanism || "none") === "none");
  return (
    <>
      <Field>
        SASL mechanism
        <Select value={form.sasl_mechanism} onChange={(event) => onChange("sasl_mechanism", event.target.value)}>
          <option value="none">None</option>
          <option value="plain">PLAIN</option>
          <option value="scram_sha_256">SCRAM-SHA-256</option>
          <option value="scram_sha_512">SCRAM-SHA-512</option>
        </Select>
      </Field>
      {secured ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            Username
            <Input value={form.username} onChange={(event) => onChange("username", event.target.value)} autoComplete="off" required />
          </Field>
          <Field>
            {editing ? "New password" : "Password"}
            <Input
              type="password"
              value={form.password}
              onChange={(event) => onChange("password", event.target.value)}
              autoComplete="new-password"
              required={passwordRequired}
              placeholder={editing ? "Leave blank to keep the encrypted password" : ""}
            />
          </Field>
        </div>
      ) : null}
    </>
  );
}
