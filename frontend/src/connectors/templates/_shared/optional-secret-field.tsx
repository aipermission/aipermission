import { Checkbox, Field, Input } from "../../../components/ui/form";

export function OptionalSecretField({
  label,
  value,
  editing,
  clear,
  placeholder,
  onChange,
  onClear,
}: {
  label: string;
  value: string;
  editing: boolean;
  clear?: boolean;
  placeholder: string;
  onChange: (_value: string) => void;
  onClear: (_clear: boolean) => void;
}) {
  return (
    <div className="grid gap-2">
      <Field>
        {label}
        <Input
          type="password"
          value={value}
          disabled={!!clear}
          autoComplete="new-password"
          placeholder={placeholder}
          onChange={(event) => onChange(event.target.value)}
        />
      </Field>
      {editing ? (
        <Field className="flex items-center gap-2">
          <Checkbox checked={!!clear} onChange={(event) => onClear(event.target.checked)} />
          Remove stored {label.replace(/^New /, "").toLowerCase()}
        </Field>
      ) : null}
    </div>
  );
}
