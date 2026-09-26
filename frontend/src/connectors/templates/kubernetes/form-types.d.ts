import type { ComponentProps } from "react";
import type { CredentialProfileForm } from "../_shared/connector-form-types";
import type { TransportConnectorIdentityFields } from "../_shared/network-transport-fields";

export type KubernetesScopeForm = { scope_mode: string; namespaces: string };
export type KubernetesCredentialForm = CredentialProfileForm & KubernetesScopeForm;
export type KubernetesConnectionForm = ComponentProps<typeof TransportConnectorIdentityFields>["form"] &
  KubernetesScopeForm & {
    kubectl_command: string;
    context: string;
    default_namespace: string;
    profile_label: string;
    risk_label: string;
  };
