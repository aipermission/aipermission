import type { ComponentProps, ComponentType, FormEvent, ReactNode } from "react";
import type { Button } from "../../components/ui/button";
import type { ConnectorFieldChange } from "../templates/_shared/connector-form-types";

export type EditorForm = { connector_kind: string; project_id?: string | number | null };
export type EditorTarget = { name?: string; project_id?: string | number | null };
export type EditorActionState = { state: string; error?: string | null };
export type ConnectorMenuCatalog = {
  state: string;
  data: { kind: string }[];
  details: Record<string, { label?: string; version?: string }>;
};
export type ConnectorFormSlotProps<Form extends EditorForm, Credential, Target extends EditorTarget, Active> = {
  form: Form;
  mode: "create" | "edit";
  credentials: Credential[];
  targets: Target[];
  activeCredential: Active | null;
  onChange: ConnectorFieldChange<Form>;
};

export type ConnectorEditorDrawerProps<Form extends EditorForm, Credential, Target extends EditorTarget, Active> = {
  drawer: { open: boolean; mode: "create" | "edit"; target: Target | null };
  form: Form;
  state: EditorActionState;
  connectorOptions: { kind: string; label: string }[];
  projects: { id: number | string; name: string }[];
  credentials: Credential[];
  targets: Target[];
  activeConnectorModel: {
    submitDisabled?: (_context: { state: EditorActionState; mode: "create" | "edit"; form: Form; credentials: Credential[] }) => boolean;
    submitLabel?: (_context: { state: EditorActionState; mode: "create" | "edit"; form: Form }) => string;
  } | null;
  activeCredential: Active | null;
  FormTemplate: ComponentType<ConnectorFormSlotProps<Form, Credential, Target, Active>> | null;
  onProjectChange: (_projectID: string) => void;
  editor: {
    closeEditor: () => void;
    save: (_event: FormEvent<HTMLFormElement>) => unknown;
    selectKind: (_kind: string) => void;
    updateField: ConnectorFieldChange<Form>;
  };
};

export type ConnectorDeleteDialog = {
  title?: string;
  description?: string;
  details?: { label: string; value?: ReactNode }[];
  notice?: ReactNode;
  actions?: {
    label: string;
    action?: string;
    variant?: ComponentProps<typeof Button>["variant"];
    pendingLabel?: string;
    removeKey?: boolean;
  }[];
};
export type DeleteConnectorDialogProps = {
  value: { open: boolean; target: EditorTarget | null };
  dialog?: ConnectorDeleteDialog | null;
  state: EditorActionState;
  onDelete: (_removeKey: boolean) => unknown;
  onClose: () => void;
};
