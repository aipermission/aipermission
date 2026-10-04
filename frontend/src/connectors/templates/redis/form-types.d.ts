import type { NetworkConnectionForm, UsernameCredentialForm } from "../_shared/connector-form-types";
import type { UsernamePasswordProfile } from "../../profile-lifecycle/types";

export type RedisConnectionForm = NetworkConnectionForm &
  Omit<UsernameCredentialForm, "target_id"> & {
    name: string;
    server_family?: string;
    database: string | number;
    tls_mode?: string;
  };

export type RedisModelForm = RedisConnectionForm & { connector_kind: string; profile_id?: string };

export type RedisProfile = UsernamePasswordProfile;

export interface RedisConfig {
  server_family?: string;
  connection_mode?: string;
  host?: string;
  port?: number;
  database?: number;
  tls_mode?: string;
  transport_target_ref?: string;
}

export interface RedisTarget {
  id?: number;
  name?: string;
  target_name?: string;
  connector_kind?: string;
  profile_label?: string;
  profiles?: RedisProfile[];
  config?: RedisConfig;
}

export type RedisPresentationTarget = Pick<RedisTarget, "name" | "target_name" | "profile_label"> & {
  config?: Omit<RedisConfig, "port" | "database"> & { port?: string | number; database?: string | number };
};
