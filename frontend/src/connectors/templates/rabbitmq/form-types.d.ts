import type { NetworkConnectionForm, UsernameCredentialForm } from "../_shared/connector-form-types";

export type RabbitMQConnectionForm = NetworkConnectionForm &
  Omit<UsernameCredentialForm, "target_id"> & {
    name: string;
    scheme?: string;
    vhost: string;
  };
