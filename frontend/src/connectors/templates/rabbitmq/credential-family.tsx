import { defineCredentialFamily } from "../_shared/credential-family-registration";
import { credentialDisplayRow } from "../_shared/credential-display-row";
import { optionalConsoleNumber, optionalConsoleText } from "../_shared/console-target-config";
import type { InventoryTarget } from "../../../lib/gateway-contracts/connector-inventory-contract";
import { RabbitMQCredentialFormTemplate } from "./credential-form";
import * as model from "./model";

type Target = ReturnType<typeof rabbitCredentialTargets>[number];
type Profile = Target["profiles"][number];
type State = ReturnType<typeof model.emptyCredentialState>;
type Row = ReturnType<typeof model.credentialRows<Profile, Target>>[number];

export const rabbitCredentialFamily = defineCredentialFamily<State, Row, Target, "create" | "update">({
  kind: "rabbitmq",
  label: "RabbitMQ",
  decodeTargets: rabbitCredentialTargets,
  emptyState: model.emptyCredentialState,
  model,
  rows: ({ targets }) => model.credentialRows<Profile, Target>({ targets }),
  displayRow: credentialDisplayRow,
  renderForm: ({ editor, targets }) => (
    <RabbitMQCredentialFormTemplate
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

export function rabbitCredentialTargets(targets: readonly InventoryTarget[]) {
  return targets
    .filter((target) => target.connector_kind === "rabbitmq")
    .map((target) => {
      const config = target.config || {};
      return {
        ...target,
        config: {
          ...config,
          host: optionalConsoleText(config.host, "RabbitMQ", "host"),
          port: optionalConsoleNumber(config.port, "RabbitMQ", "port"),
          scheme: optionalConsoleText(config.scheme, "RabbitMQ", "scheme"),
          vhost: optionalConsoleText(config.vhost, "RabbitMQ", "vhost"),
          connection_mode: optionalConsoleText(config.connection_mode, "RabbitMQ", "connection_mode"),
          transport_target_ref: optionalConsoleText(config.transport_target_ref, "RabbitMQ", "transport_target_ref"),
        },
        profiles: (target.profiles || []).map((profile) => ({
          ...profile,
          public: { ...profile.public, username: optionalConsoleText(profile.public?.username, "RabbitMQ", "username") },
        })),
      };
    });
}
