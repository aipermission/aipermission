import type { ComponentProps, FormEventHandler } from "react";
import type { CredentialProfileFields } from "./credential-profile-fields";
import type { NetworkTarget, NetworkTransportFields } from "./network-transport-fields";

export type CredentialProfileForm = ComponentProps<typeof CredentialProfileFields>["form"];
export type UsernameCredentialForm = CredentialProfileForm & { username: string; password: string };
export type NetworkConnectionForm = ComponentProps<typeof NetworkTransportFields>["form"];
export type ConnectorFormProps<Form, Value = string> = {
  form: Form;
  mode?: "create" | "edit";
  targets?: readonly NetworkTarget[];
  onChange: (_field: string, _value: Value) => void;
};
export type CredentialFormProps<Form extends CredentialProfileForm> = {
  form: Form;
  formMode?: "create" | "edit";
  targets: readonly NetworkTarget[];
  state: { state: string; error?: string | null };
  onChange: (_form: Form) => void;
  onSubmit: FormEventHandler<HTMLFormElement>;
};
