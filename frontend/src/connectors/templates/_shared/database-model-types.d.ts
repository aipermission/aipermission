import type { FormEvent } from "react";
import type { components } from "../../../../types/generated-openapi";
import type { DatabaseConnectionForm, DatabaseCredentialForm } from "./database-form-types";

type Profile = components["schemas"]["ConnectorCredentialProfile"];
type Target = components["schemas"]["ConnectorTarget"];
export type DatabaseProfile = Pick<Profile, "id" | "kind" | "label" | "risk_label"> & {
  public?: Record<string, unknown> & { username?: string };
};
export type DatabaseTarget = Pick<Target, "id" | "name" | "connector_kind"> & {
  config?: {
    host?: string;
    port?: string | number;
    database?: string;
    connection_mode?: string;
    transport_target_ref?: string;
    [key: string]: unknown;
  };
  profiles?: DatabaseProfile[];
  target_name?: string;
  profile_label?: string;
};
export type DatabaseCredentialRow = {
  id: number;
  target_id: number;
  name: string;
  profile?: DatabaseProfile;
};
export type DatabaseTargetDefaults = Pick<
  DatabaseConnectionForm,
  "name" | "host" | "port" | "database" | "connection_mode" | "transport_target_ref"
>;
export type DatabaseTransportForm = { connector_kind: string; connection_mode: string; host?: string; transport_target_ref?: string };
export type SyncedDatabaseForm<Form extends DatabaseTransportForm> = Omit<Form, "host" | "transport_target_ref"> &
  Pick<DatabaseTransportForm, "host" | "transport_target_ref">;
export type DatabaseModelForm<Fields extends DatabaseTargetDefaults> = Omit<Fields, "port" | "host" | "transport_target_ref"> &
  Omit<DatabaseCredentialForm, "target_id"> & {
    host: string;
    transport_target_ref: string;
    port: string | number;
    connector_kind: string;
    profile_id?: string;
    project_id?: string | number;
  };
export type DatabaseModelConfig<Fields extends DatabaseTargetDefaults, Credential extends DatabaseCredentialForm> = {
  kind: string;
  label: string;
  targetDefaults: Fields;
  credentialDefaults: Credential;
  defaultRiskLabel: string;
  targetForm: (_target: DatabaseTarget) => Omit<Fields, "name" | "port"> & { port: string | number };
  targetConfig: (_form: DatabaseModelForm<Fields>) => Record<string, unknown>;
  targetEndpoint: (_input: { target: DatabaseTarget }) => string;
  credentialExtras?: (_row: DatabaseCredentialRow) => Partial<Credential>;
  credentialPublic?: (_form: Credential) => Record<string, unknown>;
  targetCredentialPublic?: (_form: DatabaseModelForm<Fields>) => Record<string, unknown>;
  credentialMetadata?: (_profile: DatabaseProfile) => string[];
  includeEmptyPassword?: boolean;
};
export type CredentialFormArguments<Credential extends DatabaseCredentialForm> = {
  targets: DatabaseTarget[];
  formState: { form: Credential };
  setFormState: (_state: { form: Credential }) => void;
  formMode: "create" | "edit";
  state: { state: string; error?: string | null };
  onSubmit: (_event: FormEvent<HTMLFormElement>, _operation: "create" | "update") => unknown;
};
