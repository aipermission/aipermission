import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { Drawer } from "../../components/ui/drawer";
import { Field, Select } from "../../components/ui/form";
import { Notice } from "../../components/ui/notice";
import { supportedConnectorKinds } from "../templates/catalog";
import { ConnectorIcon, connectorKindLabel, connectorSummary } from "../templates/common";
import { ConnectorTemplateNotFound, getConnectorModel } from "../templates/registry";

export function AddConnectorMenu({ catalog, onAdd }) {
  const backendKinds = new Set(catalog.data.map((item) => item.kind));
  const availableKinds = supportedConnectorKinds.filter((kind) => backendKinds.has(kind) && catalog.details[kind]);
  return (
    <div className="absolute right-0 top-12 z-40 w-[360px] overflow-hidden rounded-lg border border-stone-200 bg-white p-2 shadow-xl dark-panel">
      <div className="grid gap-1">
        {availableKinds.map((kind) => {
          const detail = catalog.details[kind];
          return (
            <button
              type="button"
              key={kind}
              className="grid gap-2 rounded-md px-3 py-3 text-left transition hover:bg-stone-50 focus:bg-stone-50 focus:outline-none dark-panel-subtle"
              onClick={() => onAdd(kind)}
            >
              <span className="flex items-center justify-between gap-3">
                <span className="inline-flex min-w-0 items-center gap-2 font-semibold text-stone-900">
                  <ConnectorIcon kind={kind} className="h-4 w-4 shrink-0 text-stone-500" />
                  <span className="truncate">{detail?.label || connectorKindLabel(kind)}</span>
                </span>
                <Badge tone="neutral">{detail?.version || "0.1"}</Badge>
              </span>
              <span className="text-xs leading-5 text-stone-500">{connectorSummary(kind)}</span>
            </button>
          );
        })}
        {catalog.state === "loading" ? <p className="px-3 py-2 text-sm text-stone-500">Loading connector catalog...</p> : null}
        {catalog.state === "ready" && availableKinds.length === 0 ? (
          <p className="px-3 py-2 text-sm text-stone-500">
            No connector type is available in both the backend catalog and frontend templates.
          </p>
        ) : null}
      </div>
    </div>
  );
}

export function ConnectorEditorDrawer({
  drawer,
  form,
  state,
  connectorOptions,
  projects,
  credentials,
  targets,
  activeConnectorModel,
  activeCredential,
  FormTemplate,
  editor,
}) {
  return (
    <Drawer
      open={drawer.open}
      title={drawer.mode === "edit" ? `Edit ${drawer.target?.name || "connector"}` : "Add connector"}
      description={
        drawer.mode === "edit"
          ? "Update this connector and its default credential profile."
          : "Choose a connector type, then create its first default credential profile."
      }
      onClose={editor.closeEditor}
    >
      <form className="grid gap-4" onSubmit={editor.save}>
        {drawer.mode === "create" ? (
          <Field>
            Connector type
            <Select value={form.connector_kind} onChange={(event) => editor.selectKind(event.target.value)}>
              {connectorOptions.map((option) => (
                <option value={option.kind} key={option.kind}>
                  {option.label}
                </option>
              ))}
            </Select>
          </Field>
        ) : null}
        <Field>
          Project
          <Select value={form.project_id || ""} onChange={(event) => editor.updateField("project_id", event.target.value)} required>
            <option value="" disabled>
              Select project
            </option>
            {projects.map((project) => (
              <option value={project.id} key={project.id}>
                {project.name}
              </option>
            ))}
          </Select>
        </Field>
        {FormTemplate ? (
          <FormTemplate
            form={form}
            mode={drawer.mode}
            credentials={credentials}
            targets={targets.filter((target) => String(target.project_id || "") === String(form.project_id || ""))}
            activeCredential={activeCredential}
            onChange={editor.updateField}
          />
        ) : (
          <ConnectorTemplateNotFound kind={form.connector_kind} slot="form" />
        )}
        {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
        <div className="grid gap-2 sm:grid-cols-2">
          <Button type="button" variant="outline" onClick={editor.closeEditor}>
            Cancel
          </Button>
          <Button
            type="submit"
            disabled={activeConnectorModel?.submitDisabled?.({ state, mode: drawer.mode, form, credentials }) ?? state.state === "saving"}
          >
            {activeConnectorModel?.submitLabel?.({ state, mode: drawer.mode, form }) ||
              (drawer.mode === "edit" ? "Save changes" : "Create connector")}
          </Button>
        </div>
      </form>
    </Drawer>
  );
}

export function DeleteConnectorDialog({ value, state, onDelete, onClose }) {
  const target = value.target;
  const dialog = target ? getConnectorModel(target.connector_kind)?.deleteDialog?.({ target }) : null;
  const actions = dialog?.actions || [
    { label: "Cancel", action: "close", variant: "outline" },
    { label: "Delete connector", removeKey: false },
  ];
  return (
    <Dialog
      open={value.open}
      title={dialog?.title || "Delete connector"}
      description={dialog?.description || "Remove this connector target from aipermission."}
      onClose={onClose}
      size="md"
    >
      {target ? (
        <div className="grid gap-4">
          <div className="rounded-md border border-stone-200 bg-stone-50 p-3 text-sm text-stone-700">
            {(dialog?.details || [])
              .filter((item) => item.value)
              .map((item) => (
                <p className="mt-1 first:mt-0" key={item.label}>
                  <span className="font-semibold">{item.label}: </span>
                  <span className={item.label === "Reference" ? "font-mono text-xs" : ""}>{item.value}</span>
                </p>
              ))}
          </div>
          {dialog?.notice ? <Notice tone="warn">{dialog.notice}</Notice> : null}
          {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            {actions.map((action) => (
              <Button
                type="button"
                variant={action.variant || "default"}
                onClick={() => (action.action === "close" ? onClose() : onDelete(Boolean(action.removeKey)))}
                disabled={state.state === "deleting"}
                key={action.label}
              >
                {state.state === "deleting" && action.pendingLabel ? action.pendingLabel : action.label}
              </Button>
            ))}
          </div>
        </div>
      ) : null}
    </Dialog>
  );
}
