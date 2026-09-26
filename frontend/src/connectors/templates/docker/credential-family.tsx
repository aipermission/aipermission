import { defineCredentialFamily } from "../_shared/credential-family-registration";
import { credentialDisplayRow } from "../_shared/credential-display-row";
import { optionalConsoleText } from "../_shared/console-target-config";
import type { InventoryTarget } from "../../../lib/gateway-contracts/connector-inventory-contract";
import { DockerCredentialFormTemplate } from "./credential-form";
import * as model from "./model";

type Target = ReturnType<typeof dockerCredentialTargets>[number];
type Profile = Target["profiles"][number];
type State = ReturnType<typeof model.emptyCredentialState>;
type Row = ReturnType<typeof model.credentialRows<Profile, Target>>[number];

export const dockerCredentialFamily = defineCredentialFamily<State, Row, Target, "create" | "update">({
  kind: "docker",
  label: "Docker",
  decodeTargets: dockerCredentialTargets,
  emptyState: model.emptyCredentialState,
  model,
  rows: ({ targets }) => model.credentialRows<Profile, Target>({ targets }),
  displayRow: credentialDisplayRow,
  renderForm: ({ editor, targets }) => (
    <DockerCredentialFormTemplate
      {...model.credentialFormProps({
        targets,
        formState: editor.formState,
        setFormState: editor.setFormState,
        formMode: editor.drawer.mode,
        state: editor.actionState,
        onSubmit: editor.save,
      })}
    />
  ),
});

export function dockerCredentialTargets(targets: readonly InventoryTarget[]) {
  return targets
    .filter((target) => target.connector_kind === "docker")
    .map((target) => {
      const config = target.config || {};
      return {
        ...target,
        config: {
          ...config,
          connection_mode: optionalConsoleText(config.connection_mode, "Docker", "connection_mode"),
          transport_target_ref: optionalConsoleText(config.transport_target_ref, "Docker", "transport_target_ref"),
          docker_command: optionalConsoleText(config.docker_command, "Docker", "docker_command"),
        },
        profiles: (target.profiles || []).map((profile) => ({
          ...profile,
          public: {
            ...profile.public,
            scope_mode: optionalConsoleText(profile.public?.scope_mode, "Docker", "scope_mode"),
            allowed_containers: optionalConsoleText(profile.public?.allowed_containers, "Docker", "allowed_containers"),
            allowed_patterns: optionalConsoleText(profile.public?.allowed_patterns, "Docker", "allowed_patterns"),
          },
        })),
      };
    });
}
