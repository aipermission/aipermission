import { defineConnectorFamily } from "../_shared/connector-family-registration";
import { sqlServerTargets } from "./credential-family";
import { SQLServerConnectorFormTemplate } from "./form";
import * as model from "./model";

type Target = ReturnType<typeof sqlServerTargets>[number];

export const sqlServerConnectorFamily = defineConnectorFamily<
  ReturnType<typeof model.emptyForm>,
  Target["profiles"][number],
  Target,
  never,
  never,
  { open: boolean }
>({
  kind: "sqlserver",
  decodeTargets: sqlServerTargets,
  decodeCredentials: () => [],
  emptyForm: model.emptyForm,
  emptyOperation: () => ({ open: false }),
  model: { ...model, syncForm: model.syncEditorForm },
  tableModel: model,
  deleteDialog: model.deleteDialog,
  renderForm: ({ form, mode, onChange, targets }) => (
    <SQLServerConnectorFormTemplate
      form={form}
      mode={mode}
      onChange={onChange}
      passwordRequired={
        mode === "edit" &&
        form.username !==
          targets.flatMap((target) => target.profiles || []).find((profile) => String(profile.id) === form.profile_id)?.public?.username
      }
    />
  ),
  renderRowActions: () => null,
  renderOperations: () => null,
});
