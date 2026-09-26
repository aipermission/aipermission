import { defineCredentialFamily } from "../_shared/credential-family-registration";
import { credentialDisplayRow } from "../_shared/credential-display-row";
import { optionalConsoleNumber, optionalConsoleText } from "../_shared/console-target-config";
import type { InventoryTarget } from "../../../lib/gateway-contracts/connector-inventory-contract";
import type { RedisTarget } from "./form-types";
import { RedisCredentialFormTemplate } from "./credential-form";
import * as model from "./model";

type Target = RedisTarget & InventoryTarget;
type State = ReturnType<typeof model.emptyCredentialState>;
type Row = ReturnType<typeof model.credentialRows<Target>>[number];

export const redisCredentialFamily = defineCredentialFamily<State, Row, Target, "create" | "update">({
  kind: "redis",
  label: model.connectorProductLabel,
  decodeTargets: redisCredentialTargets,
  emptyState: model.emptyCredentialState,
  model,
  rows: model.credentialRows,
  displayRow: credentialDisplayRow,
  renderForm: ({ editor, targets }) => (
    <RedisCredentialFormTemplate
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

export function redisCredentialTargets(targets: readonly InventoryTarget[]): Target[] {
  return targets
    .filter((target) => target.connector_kind === "redis")
    .map((target) => {
      const config = target.config || {};
      return {
        ...target,
        config: {
          ...config,
          server_family: optionalConsoleText(config.server_family, "Redis / Valkey", "server_family"),
          host: optionalConsoleText(config.host, "Redis / Valkey", "host"),
          port: optionalConsoleNumber(config.port, "Redis / Valkey", "port"),
          database: optionalConsoleNumber(config.database, "Redis / Valkey", "database"),
          connection_mode: optionalConsoleText(config.connection_mode, "Redis / Valkey", "connection_mode"),
          tls_mode: optionalConsoleText(config.tls_mode, "Redis / Valkey", "tls_mode"),
          transport_target_ref: optionalConsoleText(config.transport_target_ref, "Redis / Valkey", "transport_target_ref"),
        },
        profiles: (target.profiles || []).map((profile) => ({
          ...profile,
          public: { ...profile.public, username: optionalConsoleText(profile.public?.username, "Redis / Valkey", "username") },
        })),
      };
    });
}
