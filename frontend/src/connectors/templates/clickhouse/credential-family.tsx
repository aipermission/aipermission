import { defineCredentialFamily } from "../_shared/credential-family-registration";
import { credentialDisplayRow } from "../_shared/credential-display-row";
import { databaseCredentialTargets } from "../_shared/database-credential-targets";
import type { InventoryTarget } from "../../../lib/gateway-contracts/connector-inventory-contract";
import { ClickHouseCredentialFormTemplate } from "./credential-form";
import * as model from "./model";

type Target = ReturnType<typeof clickHouseCredentialTargets>[number];
type State = ReturnType<typeof model.emptyCredentialState>;
type Profile = Target["profiles"][number];
type Row = ReturnType<typeof model.credentialRows<Profile, Target>>[number];

export const clickHouseCredentialFamily = defineCredentialFamily<State, Row, Target, "create" | "update">({
  kind: "clickhouse",
  label: "ClickHouse",
  decodeTargets: clickHouseCredentialTargets,
  emptyState: model.emptyCredentialState,
  model,
  rows: ({ targets }) => model.credentialRows<Profile, Target>({ targets }),
  displayRow: credentialDisplayRow,
  renderForm: ({ editor, targets }) => (
    <ClickHouseCredentialFormTemplate
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

export function clickHouseCredentialTargets(targets: readonly InventoryTarget[]) {
  return databaseCredentialTargets(targets, "clickhouse", "ClickHouse");
}
