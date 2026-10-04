import type { ComponentProps } from "react";
import type { CredentialProfileForm } from "../_shared/connector-form-types";
import type { TransportConnectorIdentityFields } from "../_shared/network-transport-fields";
import type { LifecycleTarget } from "../../profile-lifecycle/types";
import type { ConnectorRuntimeIdentity } from "../_shared/runtime-model-types";

export type DockerScopeForm = { scope_mode: string; allowed_containers: string; allowed_patterns: string };
export type DockerCredentialForm = CredentialProfileForm & DockerScopeForm;
export type DockerConnectionForm = ComponentProps<typeof TransportConnectorIdentityFields>["form"] &
  DockerScopeForm & {
    docker_command: string;
    profile_label: string;
    risk_label: string;
  };

export type DockerModelForm = DockerConnectionForm & { connector_kind: string; profile_id?: string; project_id?: string | number };
export interface DockerProfile {
  id: number;
  label: string;
  kind: string;
  risk_label?: string;
  public?: Partial<DockerScopeForm>;
}
export interface DockerTarget extends LifecycleTarget<DockerProfile> {
  target_name?: string;
  profile_label?: string;
  config?: { connection_mode?: string; transport_target_ref?: string; docker_command?: string };
}
export type DockerRuntimeTarget = DockerTarget & ConnectorRuntimeIdentity;
export type DockerPresentationTarget = Pick<DockerTarget, "config" | "target_name" | "profile_label"> & { name?: string };
