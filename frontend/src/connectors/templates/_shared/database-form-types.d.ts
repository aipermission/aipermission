import type { ComponentProps, FormEventHandler } from "react";
import type { NetworkTransportFields, NetworkTarget } from "./network-transport-fields";

export type DatabaseCredentialForm = {
  target_id: string;
  profile_label: string;
  risk_label: string;
  username: string;
  password: string;
};
export type DatabaseConnectionForm = ComponentProps<typeof NetworkTransportFields>["form"] &
  Omit<DatabaseCredentialForm, "target_id"> & { name: string; database: string };
export type DatabaseConnectionFormProps<Form extends DatabaseConnectionForm> = {
  form: Form;
  mode?: "create" | "edit";
  targets?: readonly NetworkTarget[];
  onChange: ComponentProps<typeof NetworkTransportFields>["onChange"];
};
export type DatabaseCredentialFormProps<Form extends DatabaseCredentialForm = DatabaseCredentialForm> = {
  form: Form;
  formMode?: "create" | "edit";
  targets: readonly NetworkTarget[];
  state: { state: string; error?: string };
  onChange: (_form: Form) => void;
  onSubmit: FormEventHandler<HTMLFormElement>;
};
