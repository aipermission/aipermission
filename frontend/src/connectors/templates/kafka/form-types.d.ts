import type { CredentialProfileForm } from "../_shared/connector-form-types";
import type { ComponentProps } from "react";
import type { ConnectionModeFields } from "../_shared/network-transport-fields";
import type { LifecycleTarget } from "../_shared/target-profile-lifecycle-types";

export type KafkaSASLForm = { sasl_mechanism: string; existing_sasl_mechanism?: string; username: string; password: string };
export type KafkaCredentialForm = CredentialProfileForm & KafkaSASLForm;
export type KafkaConnectionForm = ComponentProps<typeof ConnectionModeFields>["form"] &
  KafkaSASLForm & {
    name: string;
    server_family: string;
    bootstrap_brokers: string;
    project_id?: string | number;
    tls_enabled: boolean;
    tls_server_name: string;
    allow_insecure_plain_sasl: boolean;
    tls_ca_pem: string;
    profile_label: string;
    risk_label: string;
  };

export type KafkaModelForm = KafkaConnectionForm & { connector_kind: string; profile_id?: string };
export interface KafkaProfile {
  id: number;
  label: string;
  kind: string;
  risk_label?: string;
  public?: { mechanism?: string; username?: string };
}
export interface KafkaTarget extends LifecycleTarget<KafkaProfile> {
  target_name?: string;
  profile_label?: string;
  config?: {
    server_family?: string;
    connection_mode?: string;
    bootstrap_brokers?: string;
    transport_target_ref?: string;
    tls_enabled?: boolean;
    allow_insecure_plain_sasl?: boolean;
    tls_server_name?: string;
    tls_ca_pem?: string;
  };
}

export type KafkaPresentationTarget = Pick<KafkaTarget, "target_name" | "profile_label" | "config"> & { name?: string };
