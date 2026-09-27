import type { Dispatch, FormEvent, SetStateAction } from "react";
import type { ComponentProps } from "react";
import type { SSHCredentialFormTemplate } from "./credential-form";
import type { SSHCredentialState, SSHForm, SSHKey, SSHProfile, SSHTarget } from "./form-types";

export type SSHModelTarget = SSHTarget & {
  target_name?: string;
  profile_label?: string;
  public?: SSHProfile["public"];
  runtime_id?: number;
  ref?: string;
  target_id?: number;
  profile_id?: number;
};
export type SSHPresentationTarget = Pick<SSHModelTarget, "config" | "public" | "target_name" | "profile_label"> & { name?: string };
export type SSHCredentialResource = SSHKey & { resource_kind?: string; resource_ref?: string; connector_kind?: string };
export type SSHCredentialRow = {
  id: number;
  name: string;
  kind: string;
};
export type SSHFormContext = {
  form: SSHForm;
  firstCredentialID?: string | number | null;
};
export type SSHStateContext = { state: { state: string }; form: SSHForm; mode: string };
export type SSHSaveContext = { mode: string; form: SSHForm; target?: SSHModelTarget | null };
export type SSHCredentialPropsContext = {
  formState: SSHCredentialState;
  setFormState: Dispatch<SetStateAction<SSHCredentialState>>;
  formMode: "create" | "edit";
  state: { state: string; error?: string | null };
  onSubmit: (_event: FormEvent<HTMLFormElement>, _operation: "create" | "import" | "update") => unknown;
};
export type SSHCredentialFormProps = ComponentProps<typeof SSHCredentialFormTemplate>;
export type SSHDockerContainer = {
  id?: string;
  name?: string;
  status?: string;
  state?: string;
  image?: string;
  ports?: string;
  running_for?: string;
  created_at?: string;
  size?: string;
  command?: string;
  networks?: string;
  mounts?: string;
  labels?: string;
};
export type SSHHostKey = {
  host: string;
  hostname: string;
  port: number;
  public_key: string;
  fingerprint_sha256: string;
  key_type: string;
  changed?: boolean;
  existing_fingerprints?: string[];
};
export type SSHHostKeyError = { status: 409; data: { code: string; host_key: SSHHostKey } };
export type SSHHostKeyContext = {
  mode?: string;
  form?: SSHForm;
  target?: SSHModelTarget | null;
  profile?: SSHProfile | null;
  testKey?: string;
  operation?: "test" | "new-session" | "docker-check" | "docker-logs";
  container?: SSHDockerContainer;
};
export type SSHPayload = {
  name: string;
  host: string;
  port: number;
  username: string;
  ssh_key_id: number;
  profile_id: number;
  description: string;
  startup_input_after_connect: string;
  force_shell_command: string;
};
export type SSHHostKeyAction = {
  kind: "ssh";
  type: "test" | "new-session" | "docker-check" | "docker-logs" | "save" | "create";
  target?: SSHModelTarget | null;
  profile?: SSHProfile | null;
  runtimeTarget?: SSHModelTarget | null;
  testKey?: string;
  container?: SSHDockerContainer;
  projectID?: SSHForm["project_id"];
  payload?: SSHPayload;
  setupLater?: boolean;
};
