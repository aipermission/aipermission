import { defineCredentialFamily } from "../_shared/credential-family-registration";
import { credentialDisplayRow } from "../_shared/credential-display-row";
import { optionalConsoleBoolean, optionalConsoleText } from "../_shared/console-target-config";
import type { InventoryTarget } from "../../../lib/gateway-contracts/connector-inventory-contract";
import { KafkaCredentialFormTemplate } from "./credential-form";
import * as model from "./model";

type Target = ReturnType<typeof kafkaCredentialTargets>[number];
type Profile = Target["profiles"][number];
type State = ReturnType<typeof model.emptyCredentialState>;
type Row = ReturnType<typeof model.credentialRows<Profile, Target>>[number];

export const kafkaCredentialFamily = defineCredentialFamily<State, Row, Target, "create" | "update">({
  kind: "kafka",
  label: "Kafka / Redpanda",
  decodeTargets: kafkaCredentialTargets,
  emptyState: model.emptyCredentialState,
  model,
  rows: ({ targets }) => model.credentialRows<Profile, Target>({ targets }),
  displayRow: credentialDisplayRow,
  renderForm: ({ editor, targets }) => (
    <KafkaCredentialFormTemplate
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

export function kafkaCredentialTargets(targets: readonly InventoryTarget[]) {
  return targets
    .filter((target) => target.connector_kind === "kafka")
    .map((target) => {
      const config = target.config || {};
      return {
        ...target,
        config: {
          ...config,
          server_family: optionalConsoleText(config.server_family, "Kafka / Redpanda", "server_family"),
          bootstrap_brokers: optionalConsoleText(config.bootstrap_brokers, "Kafka / Redpanda", "bootstrap_brokers"),
          connection_mode: optionalConsoleText(config.connection_mode, "Kafka / Redpanda", "connection_mode"),
          transport_target_ref: optionalConsoleText(config.transport_target_ref, "Kafka / Redpanda", "transport_target_ref"),
          tls_enabled: optionalConsoleBoolean(config.tls_enabled, "Kafka / Redpanda", "tls_enabled"),
          allow_insecure_plain_sasl: optionalConsoleBoolean(
            config.allow_insecure_plain_sasl,
            "Kafka / Redpanda",
            "allow_insecure_plain_sasl",
          ),
          tls_server_name: optionalConsoleText(config.tls_server_name, "Kafka / Redpanda", "tls_server_name"),
          tls_ca_pem: optionalConsoleText(config.tls_ca_pem, "Kafka / Redpanda", "tls_ca_pem"),
        },
        profiles: (target.profiles || []).map((profile) => ({
          ...profile,
          public: {
            ...profile.public,
            mechanism: optionalConsoleText(profile.public?.mechanism, "Kafka / Redpanda", "mechanism"),
            username: optionalConsoleText(profile.public?.username, "Kafka / Redpanda", "username"),
          },
        })),
      };
    });
}
