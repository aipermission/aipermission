import { defineConnectorFamily } from "../_shared/connector-family-registration";
import { redisCredentialTargets } from "./credential-family";
import { RedisConnectorFormTemplate } from "./form";
import { RedisConnectorRowActionsTemplate } from "./list-item";
import { RedisConnectorOperationsTemplate } from "./operations";
import type { RedisModelForm } from "./form-types";
import * as model from "./model";

type Target = ReturnType<typeof redisCredentialTargets>[number];
type Profile = NonNullable<Target["profiles"]>[number];
type Operation = { open: boolean; connector_kind: string };

export const redisConnectorFamily = defineConnectorFamily<RedisModelForm, Profile, Target, never, never, Operation>({
  kind: "redis",
  decodeTargets: redisCredentialTargets,
  decodeCredentials: () => [],
  emptyForm: model.emptyForm,
  emptyOperation: () => ({ open: false, connector_kind: "redis" }),
  model,
  tableModel: model,
  deleteDialog: model.deleteDialog,
  renderForm: ({ form, mode, targets, onChange }) => (
    <RedisConnectorFormTemplate form={form} mode={mode} targets={targets} onChange={onChange} />
  ),
  renderRowActions: () => <RedisConnectorRowActionsTemplate />,
  renderOperations: () => <RedisConnectorOperationsTemplate />,
});
