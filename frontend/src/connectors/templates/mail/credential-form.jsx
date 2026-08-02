import { Mail } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Field, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { mailProtocolsEnabled } from "./helpers";
import { MailCredentialFields } from "./profile-fields";

export function MailCredentialFormTemplate({ targets, form, formMode = "create", state, onChange, onSubmit }) {
  const mailTargets = targets.filter((target) => target.connector_kind === "mail");
  const editing = formMode === "edit";
  return (
    <form className="grid gap-4" onSubmit={onSubmit}>
      {mailTargets.length === 0 ? <Notice tone="warn">Create a Mail connector target before adding a mailbox credential profile.</Notice> : null}
      <Field>
        Connector target
        <Select value={form.target_id} onChange={(event) => onChange({ ...form, target_id: event.target.value })} disabled={editing} required>
          <option value="" disabled>Select Mail target</option>
          {mailTargets.map((target) => <option value={target.id} key={target.id}>{target.name} · {target.config?.imap_host}:{target.config?.imap_port || 993}</option>)}
        </Select>
      </Field>
      <MailCredentialFields form={form} editing={editing} onChange={(field, value) => onChange({ ...form, [field]: value })} />
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      <Button type="submit" disabled={state.state === "saving" || mailTargets.length === 0 || !mailProtocolsEnabled(form)}>
        <Mail className="h-4 w-4" />
        {state.state === "saving" ? "Saving..." : editing ? "Save mail credential" : "Create mail credential"}
      </Button>
    </form>
  );
}
