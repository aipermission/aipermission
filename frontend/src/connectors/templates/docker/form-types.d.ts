import type { ComponentProps } from "react";
import type { CredentialProfileForm } from "../_shared/connector-form-types";
import type { TransportConnectorIdentityFields } from "../_shared/network-transport-fields";

export type DockerScopeForm = { scope_mode: string; allowed_containers: string; allowed_patterns: string };
export type DockerCredentialForm = CredentialProfileForm & DockerScopeForm;
export type DockerConnectionForm = ComponentProps<typeof TransportConnectorIdentityFields>["form"] &
  DockerScopeForm & {
    docker_command: string;
    profile_label: string;
    risk_label: string;
  };
