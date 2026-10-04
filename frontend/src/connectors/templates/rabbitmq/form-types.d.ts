import type { NetworkConnectionForm, UsernameCredentialForm } from "../_shared/connector-form-types";
import type { UsernamePasswordProfile } from "../../profile-lifecycle/types";

export type RabbitMQConnectionForm = NetworkConnectionForm &
  Omit<UsernameCredentialForm, "target_id"> & {
    name: string;
    scheme?: string;
    vhost: string;
  };

export type RabbitMQModelForm = RabbitMQConnectionForm & { connector_kind: string; profile_id?: string };
export type RabbitMQProfile = UsernamePasswordProfile;
export interface RabbitMQTarget {
  id?: number;
  name?: string;
  connector_kind?: string;
  target_name?: string;
  profile_label?: string;
  profiles?: RabbitMQProfile[];
  config?: {
    connection_mode?: string;
    scheme?: string;
    host?: string;
    port?: number;
    vhost?: string;
    transport_target_ref?: string;
  };
}

export type RabbitMQPresentationTarget = Pick<RabbitMQTarget, "name" | "target_name" | "profile_label"> & {
  config?: Omit<NonNullable<RabbitMQTarget["config"]>, "port"> & { port?: string | number };
};
