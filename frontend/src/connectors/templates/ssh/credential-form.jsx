import { Plus, Upload } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Field, Input, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";

const privateKeyPlaceholder = "-----BEGIN OPENSSH " + "PRIVATE KEY-----";

export function SSHCredentialFormTemplate({
  formMode = "create",
  mode,
  form,
  importForm,
  state,
  onModeChange,
  onFormChange,
  onImportFormChange,
  onReadImportFile,
  onCreate,
  onImport,
  onUpdate,
}) {
  if (formMode === "edit") {
    return (
      <form className="grid gap-4" onSubmit={onUpdate}>
        <Field>
          Name
          <Input value={form.name} onChange={(event) => onFormChange({ ...form, name: event.target.value })} required />
        </Field>
        <Notice>
          SSH credential edits only change the local credential label and install-command comment. Key material is not rewritten; import a new credential to
          rotate keys.
        </Notice>
        {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
        <Button type="submit" disabled={state.state === "saving"}>
          {state.state === "saving" ? "Saving..." : "Save SSH credential"}
        </Button>
      </form>
    );
  }

  return (
    <div className="grid gap-4">
      <div className="grid grid-cols-2 gap-2 rounded-md bg-stone-100 p-1">
        {[
          ["generate", "Generate"],
          ["import", "Import"],
        ].map(([value, label]) => (
          <button
            className={`rounded-md px-3 py-2 text-sm font-semibold transition ${
              mode === value ? "bg-white text-emerald-950 shadow-sm" : "text-stone-500 hover:text-stone-900"
            }`}
            key={value}
            type="button"
            onClick={() => onModeChange(value)}
          >
            {label}
          </button>
        ))}
      </div>
      {mode === "generate" ? (
        <form className="grid gap-4" onSubmit={onCreate}>
          <Field>
            Name
            <Input value={form.name} onChange={(event) => onFormChange({ ...form, name: event.target.value })} required />
          </Field>
          <div className="grid grid-cols-2 gap-2 rounded-md bg-stone-100 p-1">
            {["ed25519", "rsa"].map((type) => (
              <button
                className={`rounded-md px-3 py-2 text-sm font-semibold transition ${
                  form.key_type === type ? "bg-white text-emerald-950 shadow-sm" : "text-stone-500 hover:text-stone-900"
                }`}
                key={type}
                type="button"
                onClick={() => onFormChange({ ...form, key_type: type })}
              >
                {type}
              </button>
            ))}
          </div>
          {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
          <Button type="submit" disabled={state.state === "saving"}>
            <Plus className="h-4 w-4" />
            {state.state === "saving" ? "Creating..." : `Generate ${form.key_type} credential`}
          </Button>
          <Notice>After creating the credential, copy the install command from the table and paste it on the server.</Notice>
          <Field>
            Install command preview
            <Textarea
              readOnly
              rows={4}
              className="font-mono text-xs"
              value={`mkdir -p ~/.ssh && chmod 700 ~/.ssh && printf '%s\\n' 'ssh-${form.key_type} ... aipermission-${form.name || "key"}' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys`}
            />
          </Field>
        </form>
      ) : (
        <form className="grid gap-4" onSubmit={onImport}>
          <Field>
            Name
            <Input value={importForm.name} onChange={(event) => onImportFormChange({ ...importForm, name: event.target.value })} required />
          </Field>
          <Field>
            Private key
            <Textarea
              value={importForm.private_key}
              onChange={(event) => onImportFormChange({ ...importForm, private_key: event.target.value })}
              rows={9}
              className="font-mono text-xs"
              placeholder={privateKeyPlaceholder}
              required
            />
          </Field>
          <div className="flex flex-wrap items-center gap-2">
            <Button type="button" variant="outline" asChild>
              <label>
                <Upload className="h-4 w-4" />
                Choose key file
                <input className="hidden" type="file" onChange={onReadImportFile} />
              </label>
            </Button>
            <span className="text-xs text-stone-500">The file is read locally and imported only after you press Import.</span>
          </div>
          <Field>
            Passphrase
            <Input
              type="password"
              value={importForm.passphrase}
              onChange={(event) => onImportFormChange({ ...importForm, passphrase: event.target.value })}
              placeholder="Only needed for passphrase-protected keys"
            />
          </Field>
          {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
          <Button type="submit" disabled={state.state === "importing"}>
            <Upload className="h-4 w-4" />
            {state.state === "importing" ? "Importing..." : "Import credential"}
          </Button>
          <Notice tone="warn">
            Imported keys are decrypted once during import, normalized, and then stored in the encrypted local vault.
            The passphrase is not saved.
          </Notice>
        </form>
      )}
    </div>
  );
}
