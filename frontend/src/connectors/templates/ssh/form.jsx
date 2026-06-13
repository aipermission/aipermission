import { Link } from "react-router-dom";
import { Checkbox, Field, Input, Select, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { InstallCommandPanel } from "../common";

export function SSHConnectorFormTemplate({ form, credentials, activeCredential, onChange }) {
  return (
    <>
      {credentials.length === 0 ? (
        <Notice tone="warn">
          Create or import an SSH credential before adding an SSH connector. <Link to="/credentials" className="font-semibold underline">Open Credentials</Link>.
        </Notice>
      ) : null}
      <Notice tone="good">
        The first SSH credential profile is created from the selected username and gateway key. You can grant token permissions for this connector from Console or Tokens.
      </Notice>
      <Field>
        Connector name
        <Input value={form.name} onChange={(event) => onChange("name", event.target.value)} placeholder="worker-1" required />
      </Field>
      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_120px]">
        <Field>
          Host
          <Input value={form.host} onChange={(event) => onChange("host", event.target.value)} placeholder="203.0.113.10" required />
        </Field>
        <Field>
          Port
          <Input type="number" min="1" max="65535" value={form.port} onChange={(event) => onChange("port", event.target.value)} required />
        </Field>
      </div>
      <Field>
        Username
        <Input value={form.username} onChange={(event) => onChange("username", event.target.value)} required />
      </Field>
      <Field>
        Credential profile key
        <Select value={form.ssh_key_id} onChange={(event) => onChange("ssh_key_id", event.target.value)} required>
          <option value="" disabled>
            Select key
          </option>
          {credentials.map((key) => (
            <option value={key.id} key={key.id}>
              {key.name} · {key.key_type}
            </option>
          ))}
        </Select>
      </Field>
      {activeCredential ? (
        <InstallCommandPanel
          command={activeCredential.install_command}
          title="Install this SSH key on the target"
          description="Paste this command on the remote server before creating a tested SSH connector."
        />
      ) : null}
      <label className="flex items-start gap-3 rounded-md border border-stone-200 bg-stone-50 p-3 text-sm text-stone-700">
        <Checkbox checked={form.setup_later} onChange={(event) => onChange("setup_later", event.target.checked)} />
        <span>
          <span className="block font-semibold text-stone-900">I will install the key later</span>
          <span className="mt-1 block text-xs text-stone-500">Create the connector without testing the SSH connection yet.</span>
        </span>
      </label>
      <Field>
        Description
        <Textarea value={form.description} onChange={(event) => onChange("description", event.target.value)} rows={3} />
      </Field>
      <div className="grid gap-3 rounded-lg border border-stone-200 bg-stone-50 p-3 dark-soft-panel">
        <div>
          <p className="text-sm font-semibold text-stone-900">Advanced SSH startup</p>
          <p className="mt-1 text-xs text-stone-500">Optional startup settings for appliances that show an interactive menu before a normal shell.</p>
        </div>
        <Field>
          Startup input after connect
          <Textarea
            value={form.startup_input_after_connect}
            onChange={(event) => onChange("startup_input_after_connect", event.target.value)}
            rows={3}
            className="font-mono text-xs"
            placeholder={"q\n"}
          />
          <span className="text-xs text-stone-500">
            Sent exactly to the PTY after the shell starts. For some QNAP menus, enter <span className="font-mono">q</span> then a newline.
          </span>
        </Field>
        <Field>
          Force shell command
          <Input value={form.force_shell_command} onChange={(event) => onChange("force_shell_command", event.target.value)} placeholder="/bin/sh -l" />
          <span className="text-xs text-stone-500">
            Leave empty for normal shell startup. Use only when the target needs a specific shell command.
          </span>
        </Field>
      </div>
    </>
  );
}
