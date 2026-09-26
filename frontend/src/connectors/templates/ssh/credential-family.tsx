import { defineCredentialFamily } from "../_shared/credential-family-registration";
import { credentialDisplayRow } from "../_shared/credential-display-row";
import type { InventoryTarget } from "../../../lib/gateway-contracts/connector-inventory-contract";
import { optionalConsolePort, optionalConsoleText, optionalConsoleTextOrNumber } from "../_shared/console-target-config";
import { SSHCredentialFormTemplate } from "./credential-form";
import { SSHCredentialRowActionsTemplate } from "./credential-row-actions";
import { sshCredentialResourcesResponse } from "./model-helpers";
import * as model from "./model";
import type { SSHCredentialState } from "./form-types";
import type { SSHModelTarget } from "./model-types";

type Row = ReturnType<typeof model.credentialRows>[number];

export const sshCredentialFamily = defineCredentialFamily<SSHCredentialState, Row, SSHModelTarget, "create" | "import" | "update">({
  kind: "ssh",
  label: "SSH",
  decodeTargets: sshCredentialTargets,
  emptyState: model.emptyCredentialState,
  model,
  rows: ({ targets, credentials }) =>
    model.credentialRows({
      targets,
      credentials: sshCredentialResourcesResponse(credentials.filter((credential) => credential.connector_kind === "ssh")),
    }),
  displayRow: credentialDisplayRow,
  renderOperations: (row) => <SSHCredentialRowActionsTemplate row={row} />,
  renderForm: ({ editor }) => (
    <SSHCredentialFormTemplate
      {...model.credentialFormProps({
        formState: editor.formState,
        setFormState: editor.setFormState,
        formMode: editor.drawer.mode,
        state: editor.actionState,
        onSubmit: editor.save,
      })}
    />
  ),
});

export function sshCredentialTargets(targets: readonly InventoryTarget[]) {
  return targets
    .filter((target) => target.connector_kind === "ssh")
    .map((target) => {
      const config = target.config || {};
      return {
        ...target,
        config: {
          ...config,
          host: optionalConsoleText(config.host, "SSH", "host"),
          port: optionalConsolePort(config.port, "SSH"),
          description: optionalConsoleText(config.description, "SSH", "description"),
          startup_input_after_connect: optionalConsoleText(config.startup_input_after_connect, "SSH", "startup_input_after_connect"),
          force_shell_command: optionalConsoleText(config.force_shell_command, "SSH", "force_shell_command"),
        },
        profiles: (target.profiles || []).map((profile) => ({
          ...profile,
          public: {
            ...profile.public,
            username: optionalConsoleText(profile.public?.username, "SSH", "username"),
            ssh_key_id: optionalConsoleTextOrNumber(profile.public?.ssh_key_id, "SSH", "ssh_key_id"),
          },
        })),
      };
    });
}
