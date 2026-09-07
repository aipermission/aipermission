import { RefreshCcw, UserPlus } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Dialog } from "../../../components/ui/dialog";
import { Checkbox, Field, Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { ProvisionScopePicker } from "./provision-scope-picker";
import { usePostgresProvisioning } from "./use-postgres-provisioning";

export function ProvisionUserDialog({ value, onClose, onOperationComplete }) {
  const controller = usePostgresProvisioning({ value, onOperationComplete });
  return (
    <Dialog
      open={value.open}
      title={value.target ? `${value.target.name} managed DB user` : "Create managed DB user"}
      description="Create a scoped Postgres role with a random password and save it as an encrypted credential profile."
      onClose={onClose}
      closeDisabled={controller.state.state === "running"}
      size="xl"
      className="!w-[calc(100vw-300px)] !max-w-none h-[calc(100vh-200px)] grid-rows-[auto_minmax(0,1fr)]"
      bodyClassName="min-h-0 overflow-hidden"
    >
      <form className="grid h-full min-h-0 grid-rows-[auto_auto_minmax(0,1fr)_auto_auto] gap-4" onSubmit={controller.provisionUser}>
        <Notice tone="warn">
          AIPermission will create the database role through this admin profile. Deleting the managed credential reassigns objects owned by
          that role to the admin role, removes the managed role's privileges, and drops the managed role.
        </Notice>
        <ProvisionFields controller={controller} />
        <ProvisionScope controller={controller} />
        {controller.state.error ? (
          <Notice tone={controller.state.state === "ready" ? "warn" : "bad"}>{controller.state.error}</Notice>
        ) : null}
        {controller.metadata.state === "error" ? <Notice tone="bad">{controller.metadata.error}</Notice> : null}
        {controller.metadata.state === "pending" ? <Notice tone="warn">{controller.metadata.error}</Notice> : null}
        {controller.state.result ? (
          <Notice tone="good">
            {controller.state.result.result?.display_text || "Managed Postgres credential created."} New profile:{" "}
            {controller.state.result.profile?.label}
          </Notice>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose} disabled={controller.state.state === "running"}>
            Close
          </Button>
          <Button type="submit" disabled={!controller.canSubmit || controller.state.state === "running"}>
            <UserPlus className="h-4 w-4" />
            {controller.state.state === "running" ? "Creating user" : "Create user"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function ProvisionFields({ controller }) {
  return (
    <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_180px]">
      <Field>
        Role name
        <Input
          value={controller.form.role_name}
          onChange={(event) => controller.updateForm("role_name", event.target.value)}
          placeholder="app_reader"
          required
        />
      </Field>
      <Field>
        Profile label
        <Input
          value={controller.form.profile_label}
          onChange={(event) => controller.updateForm("profile_label", event.target.value)}
          placeholder={controller.form.role_name || "app_reader"}
        />
      </Field>
      <Field>
        Preset
        <Select value={controller.form.preset} onChange={(event) => controller.updateForm("preset", event.target.value)}>
          <option value="read_only">Read only</option>
          <option value="read_write">Read and change</option>
        </Select>
      </Field>
    </div>
  );
}

function ProvisionScope({ controller }) {
  return (
    <section className="grid min-h-0 gap-3 rounded-lg border border-stone-200 bg-stone-50 p-3 lg:grid-cols-2">
      <div className="grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] gap-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h4 className="text-sm font-semibold text-stone-900">Access scope</h4>
            <p className="text-xs text-stone-500">Choose all schemas, or narrow the role to selected schemas, tables, and columns.</p>
          </div>
          <Button
            type="button"
            variant="outline"
            className="h-8 px-3 text-xs"
            onClick={controller.loadMetadata}
            disabled={!controller.targetRef || controller.metadata.state === "loading"}
          >
            <RefreshCcw className="h-3.5 w-3.5" />
            {controller.metadata.state === "loading" ? "Loading" : "Refresh"}
          </Button>
        </div>
        <label className="flex items-start gap-3 rounded-md border border-stone-200 bg-white p-3 text-sm">
          <Checkbox
            aria-label="Select all schemas, tables, and columns"
            checked={controller.scope.all_schemas}
            onChange={(event) => controller.setScope((current) => ({ ...current, all_schemas: event.target.checked }))}
          />
          <span>
            <span className="block font-semibold text-stone-900">All schemas, all tables, all columns</span>
            <span className="text-xs text-stone-500">Grant the preset across every non-system schema visible to the admin profile.</span>
          </span>
        </label>
        {controller.scope.all_schemas ? (
          <div className="min-h-0" />
        ) : (
          <ProvisionScopePicker
            metadata={controller.metadata}
            scope={controller.scope}
            onChange={controller.setScope}
            preset={controller.form.preset}
          />
        )}
      </div>
      <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-3">
        <Notice tone="good" className="max-h-[200px] overflow-auto">
          {controller.scopeSummary}
        </Notice>
        <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
          <div className="flex items-center justify-between gap-3">
            <h4 className="text-sm font-semibold text-stone-900">SQL preview</h4>
            <CopyButton value={controller.sqlPreview} variant="outline" className="h-8 px-3 text-xs" title="Copy SQL preview" />
          </div>
          <TerminalBlock surface="log" className="min-h-0 text-xs">
            {controller.sqlPreview}
          </TerminalBlock>
        </div>
      </div>
    </section>
  );
}
