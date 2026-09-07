import { Copy, RotateCw, Trash2 } from "lucide-react";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Field, Input, Select, Textarea } from "../ui/form";
import { Notice } from "../ui/notice";
import { vaultGeneratorKinds } from "./vault-options";

export function VaultValueDialogs({ owner }) {
  return (
    <>
      <VaultRevealDialog owner={owner} />
      <VaultReplaceDialog owner={owner} />
      <VaultDeleteDialog owner={owner} />
    </>
  );
}

function VaultRevealDialog({ owner }) {
  const { reveal } = owner;
  return (
    <Dialog
      open={reveal.open}
      title={`Reveal ${reveal.item?.name || "Vault item"}`}
      description="The value is visible only in this local dialog and clears after 30 seconds."
      onClose={owner.closeReveal}
      size="lg"
      autoFocusClose={false}
    >
      <div className="grid gap-4">
        {reveal.state === "loading" ? <Notice>Decrypting after audit...</Notice> : null}
        {reveal.error ? <Notice tone="bad">{reveal.error}</Notice> : null}
        {reveal.value ? (
          <div className="grid gap-2">
            <Textarea readOnly autoFocus className="min-h-36 font-mono" value={reveal.value} />
            <div className="flex justify-end">
              <Button type="button" onClick={owner.copyRevealedValue}>
                <Copy className="h-4 w-4" />
                {reveal.copied ? "Copied" : "Copy value"}
              </Button>
            </div>
          </div>
        ) : null}
      </div>
    </Dialog>
  );
}

function VaultReplaceDialog({ owner }) {
  const { replace, setReplace } = owner;
  return (
    <Dialog
      open={replace.open}
      title="Replace local value"
      description="Import a new value or preview a locally generated value before saving it. This does not rotate the credential at its provider."
      onClose={() => replace.state !== "saving" && owner.closeReplace()}
      size="lg"
      autoFocusClose={false}
    >
      <form className="grid gap-4" onSubmit={owner.replaceValue}>
        <div className="grid grid-cols-2 rounded-md border border-stone-300 p-1">
          <Button
            type="button"
            variant={replace.source === "imported" ? "default" : "ghost"}
            className="h-9"
            onClick={owner.selectImportedReplacement}
          >
            Import value
          </Button>
          <Button
            type="button"
            variant={replace.source === "generated" ? "default" : "ghost"}
            className="h-9"
            onClick={() => void owner.generateReplacementPreview(replace.item, replace.generator_kind)}
          >
            Generate locally
          </Button>
        </div>
        {replace.source === "imported" ? (
          <Field>
            New value
            <Textarea
              autoFocus
              className="min-h-36 font-mono"
              value={replace.value}
              onChange={(event) => setReplace((current) => ({ ...current, value: event.target.value }))}
              required
            />
          </Field>
        ) : (
          <GeneratedReplacement owner={owner} />
        )}
        {replace.error ? <Notice tone="bad">{replace.error}</Notice> : null}
        <div className="grid gap-2 sm:grid-cols-2">
          <Button type="button" variant="outline" disabled={replace.state === "saving"} onClick={owner.closeReplace}>
            Cancel
          </Button>
          <Button
            type="submit"
            disabled={
              (replace.source === "imported" && !replace.value) ||
              (replace.source === "generated" && !replace.preview_token) ||
              replace.state === "saving"
            }
          >
            {replace.state === "saving" ? "Replacing..." : replace.source === "generated" ? "Save generated value" : "Replace local value"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function GeneratedReplacement({ owner }) {
  const { replace } = owner;
  return (
    <div className="grid gap-3">
      <Field>
        Generator
        <Select
          autoFocus
          value={replace.generator_kind}
          disabled={replace.preview_state === "loading" || replace.state === "saving"}
          onChange={(event) => void owner.generateReplacementPreview(replace.item, event.target.value)}
        >
          {vaultGeneratorKinds.map(([value, label]) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </Select>
      </Field>
      <Field>
        Generated value
        <Textarea
          readOnly
          className="min-h-28 font-mono"
          value={replace.preview_value}
          placeholder={replace.preview_state === "loading" ? "Generating locally..." : "Choose Generate locally to create a preview."}
        />
      </Field>
      <div className="flex items-center justify-between gap-3">
        <p className="text-xs text-stone-500">The displayed value clears after 30 seconds and is not saved until you confirm.</p>
        <Button
          type="button"
          variant="outline"
          disabled={replace.preview_state === "loading" || replace.state === "saving"}
          onClick={() => void owner.generateReplacementPreview(replace.item, replace.generator_kind)}
        >
          <RotateCw className="h-4 w-4" />
          {replace.preview_state === "loading" ? "Generating..." : "Regenerate"}
        </Button>
      </div>
    </div>
  );
}

function VaultDeleteDialog({ owner }) {
  const { remove, setRemove } = owner;
  return (
    <Dialog
      open={remove.open}
      title="Delete Vault item"
      description="Deletion cannot erase existing backups, remote process environments, logs, or snapshots."
      onClose={owner.closeRemove}
      size="lg"
      autoFocusClose={false}
    >
      <div className="grid gap-4">
        <Notice tone="warn">
          Type <strong>{remove.item?.name}</strong> to permanently delete this item from the active database.
        </Notice>
        <Input
          autoFocus
          value={remove.confirm}
          onChange={(event) => setRemove((current) => ({ ...current, confirm: event.target.value }))}
        />
        {remove.error ? <Notice tone="bad">{remove.error}</Notice> : null}
        <div className="grid gap-2 sm:grid-cols-2">
          <Button type="button" variant="outline" onClick={owner.closeRemove}>
            Cancel
          </Button>
          <Button
            type="button"
            variant="danger"
            disabled={remove.confirm !== remove.item?.name || remove.state === "deleting"}
            onClick={owner.deleteItem}
          >
            <Trash2 className="h-4 w-4" />
            {remove.state === "deleting" ? "Deleting..." : "Delete Vault item"}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
