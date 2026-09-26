import type { NetworkConnectionForm, UsernameCredentialForm } from "../_shared/connector-form-types";

export type RedisConnectionForm = NetworkConnectionForm &
  Omit<UsernameCredentialForm, "target_id"> & {
    name: string;
    server_family?: string;
    database: string | number;
    tls_mode?: string;
  };
