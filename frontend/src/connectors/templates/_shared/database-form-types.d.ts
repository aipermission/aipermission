import type { ConnectorFormProps, CredentialFormProps, NetworkConnectionForm, UsernameCredentialForm } from "./connector-form-types";

export type DatabaseCredentialForm = UsernameCredentialForm;
export type DatabaseConnectionForm = NetworkConnectionForm & Omit<DatabaseCredentialForm, "target_id"> & { name: string; database: string };
export type DatabaseConnectionFormProps<Form extends DatabaseConnectionForm> = ConnectorFormProps<Form>;
export type DatabaseCredentialFormProps<Form extends DatabaseCredentialForm = DatabaseCredentialForm> = CredentialFormProps<Form>;
