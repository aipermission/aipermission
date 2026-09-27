import type { ComponentProps } from "react";
import type { CredentialProfileForm } from "../_shared/connector-form-types";
import type { TransportConnectorIdentityFields } from "../_shared/network-transport-fields";
import type { LifecycleTarget } from "../_shared/target-profile-lifecycle-types";
import type { ConnectorRuntimeIdentity } from "../_shared/runtime-model-types";

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

export type KubernetesModelForm = KubernetesConnectionForm & { connector_kind: string; profile_id?: string; project_id?: string | number };
export interface KubernetesProfile {
  id: number;
  label: string;
  kind: string;
  risk_label?: string;
  public?: Partial<KubernetesScopeForm>;
}
export interface KubernetesTarget extends LifecycleTarget<KubernetesProfile> {
  target_name?: string;
  profile_label?: string;
  config?: {
    connection_mode?: string;
    transport_target_ref?: string;
    kubectl_command?: string;
    context?: string;
    default_namespace?: string;
  };
}
export type KubernetesRuntimeTarget = KubernetesTarget & ConnectorRuntimeIdentity;
export type KubernetesPresentationTarget = Pick<KubernetesTarget, "config" | "target_name" | "profile_label"> & { name?: string };
