import type { CredentialProfileForm } from "../_shared/connector-form-types";
import type { ComponentProps } from "react";
import type { ConnectionModeFields } from "../_shared/network-transport-fields";

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
