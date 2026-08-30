import { Field, Input, Select } from "../../../components/ui/form";

export function CredentialProfileFields({ targets, form, editing, onChange, targetPlaceholder, targetOptionLabel }) {
  function update(field, value) {
    onChange({ ...form, [field]: value });
  }

  return (
    <>
      <Field>
        Connector target
        <Select value={form.target_id} onChange={(event) => update("target_id", event.target.value)} disabled={editing} required>
          <option value="" disabled>
            {targetPlaceholder}
          </option>
          {targets.map((target) => (
            <option value={target.id} key={target.id}>
              {targetOptionLabel(target)}
            </option>
          ))}
        </Select>
      </Field>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field>
          Profile label
          <Input value={form.profile_label} onChange={(event) => update("profile_label", event.target.value)} required />
        </Field>
        <Field>
          Risk label
          <Input value={form.risk_label} onChange={(event) => update("risk_label", event.target.value)} />
        </Field>
      </div>
    </>
  );
}
