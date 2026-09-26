import { Field, Input, Select } from "../../../components/ui/form";

type CredentialForm = { target_id: string; profile_label: string; risk_label: string };
type CredentialTarget = { id: string | number };

export function CredentialProfileFields<Form extends CredentialForm, Target extends CredentialTarget>({
  targets,
  form,
  editing,
  onChange,
  targetPlaceholder,
  targetOptionLabel,
}: {
  targets: readonly Target[];
  form: Form;
  editing: boolean;
  onChange: (_form: Form) => void;
  targetPlaceholder: string;
  targetOptionLabel: (_target: Target) => string;
}) {
  function update(field: keyof CredentialForm, value: string) {
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
