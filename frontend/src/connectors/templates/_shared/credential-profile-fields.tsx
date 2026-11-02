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
      <CredentialProfileLabelFields form={form} onChange={update} />
    </>
  );
}

export function CredentialProfileLabelFields({
  form,
  onChange,
}: {
  form: Pick<CredentialForm, "profile_label" | "risk_label">;
  onChange: (_field: "profile_label" | "risk_label", _value: string) => void;
}) {
  const fields = [
    { key: "profile_label", label: "Profile label", required: true },
    { key: "risk_label", label: "Risk label", required: false },
  ] as const;
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      {fields.map(({ key, label, required }) => (
        <Field key={key}>
          {label}
          <Input value={form[key]} onChange={(event) => onChange(key, event.target.value)} required={required} />
        </Field>
      ))}
    </div>
  );
}
