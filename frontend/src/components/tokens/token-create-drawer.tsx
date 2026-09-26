import { Plus } from "lucide-react";
import type { Dispatch, FormEvent, SetStateAction } from "react";
import type { AsyncActionState } from "../../lib/use-async-action";
import { Button } from "../ui/button";
import { Drawer } from "../ui/drawer";
import { Field, Input, Select } from "../ui/form";
import { Notice } from "../ui/notice";

type TokenForm = typeof emptyForm;
const tokenExpiryOptions = [
  { value: "never", label: "Never expires", ms: 0 },
  { value: "1h", label: "1 hour", ms: 60 * 60 * 1000 },
  { value: "4h", label: "4 hours", ms: 4 * 60 * 60 * 1000 },
  { value: "1d", label: "1 day", ms: 24 * 60 * 60 * 1000 },
  { value: "7d", label: "7 days", ms: 7 * 24 * 60 * 60 * 1000 },
];

export const emptyForm = { name: "cursor-maintenance", expires_in: "never" };
export function TokenCreateDrawer({
  open,
  form,
  setForm,
  state,
  onClose,
  onSubmit,
}: {
  open: boolean;
  form: TokenForm;
  setForm: Dispatch<SetStateAction<TokenForm>>;
  state: AsyncActionState;
  onClose: () => void;
  onSubmit: (_event: FormEvent<HTMLFormElement>) => Promise<void>;
}) {
  return (
    <Drawer
      open={open}
      title="Add API token"
      description="Use one token per AI client, laptop, or temporary maintenance session."
      onClose={onClose}
      closeDisabled={state.state === "saving"}
    >
      <form className="grid gap-4" onSubmit={onSubmit}>
        <Field>
          Name
          <Input
            value={form.name}
            onChange={(event) => setForm({ ...form, name: event.target.value })}
            required
            disabled={state.state === "saving"}
          />
        </Field>
        <Field>
          Expiration
          <Select
            value={form.expires_in}
            onChange={(event) => setForm({ ...form, expires_in: event.target.value })}
            disabled={state.state === "saving"}
          >
            {tokenExpiryOptions.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </Select>
        </Field>
        {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
        <Button type="submit" disabled={state.state === "saving"}>
          <Plus className="h-4 w-4" />
          {state.state === "saving" ? "Creating..." : "Create token"}
        </Button>
        <Notice>Use short-lived tokens for temporary maintenance. By default the token is shown once after creation.</Notice>
      </form>
    </Drawer>
  );
}

export function tokenCreatePayload(form: TokenForm) {
  const payload: { name: string; expires_at?: string } = { name: form.name };
  const option = tokenExpiryOptions.find((item) => item.value === form.expires_in);
  if (option?.ms) {
    payload.expires_at = new Date(Date.now() + option.ms).toISOString();
  }
  return payload;
}
