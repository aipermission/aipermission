import { defineCredentialFamily } from "../_shared/credential-family-registration";
import { credentialDisplayRow } from "../_shared/credential-display-row";
import { optionalConsoleBoolean, optionalConsolePort, optionalConsoleText } from "../_shared/console-target-config";
import type { InventoryTarget } from "../../../lib/gateway-contracts/connector-inventory-contract";
import { S3CredentialFormTemplate } from "./credential-form";
import * as model from "./model";

type Target = ReturnType<typeof s3CredentialTargets>[number];
type Profile = Target["profiles"][number];
type State = ReturnType<typeof model.emptyCredentialState>;
type Row = ReturnType<typeof model.credentialRows<Profile, Target>>[number];

export const s3CredentialFamily = defineCredentialFamily<State, Row, Target, "create" | "update">({
  kind: "s3",
  label: "S3",
  decodeTargets: s3CredentialTargets,
  emptyState: model.emptyCredentialState,
  model,
  rows: ({ targets }) => model.credentialRows<Profile, Target>({ targets }),
  displayRow: (row) => credentialDisplayRow({ ...row, metadata: row.metadata ? [row.metadata] : [] }),
  renderForm: ({ editor, targets }) => (
    <S3CredentialFormTemplate
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

export function s3CredentialTargets(targets: readonly InventoryTarget[]) {
  return targets
    .filter((target) => target.connector_kind === "s3")
    .map((target) => {
      const config = target.config || {};
      return {
        ...target,
        config: {
          ...config,
          host: optionalConsoleText(config.host, "S3", "host"),
          port: optionalConsolePort(config.port, "S3"),
          scheme: optionalConsoleText(config.scheme, "S3", "scheme"),
          region: optionalConsoleText(config.region, "S3", "region"),
          bucket: optionalConsoleText(config.bucket, "S3", "bucket"),
          connection_mode: optionalConsoleText(config.connection_mode, "S3", "connection_mode"),
          transport_target_ref: optionalConsoleText(config.transport_target_ref, "S3", "transport_target_ref"),
          path_style: optionalConsoleBoolean(config.path_style, "S3", "path_style"),
          trust_conditional_requests: optionalConsoleBoolean(config.trust_conditional_requests, "S3", "trust_conditional_requests"),
        },
        profiles: (target.profiles || []).map((profile) => ({
          ...profile,
          public: {
            ...profile.public,
            access_key_id: optionalConsoleText(profile.public?.access_key_id, "S3", "access_key_id"),
          },
        })),
      };
    });
}
