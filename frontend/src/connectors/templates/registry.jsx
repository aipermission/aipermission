import { Notice } from "../../components/ui/notice";
import { getConnectorMetadata } from "./catalog";
import { PostgresConnectorConsoleTemplate, PostgresConnectorToolbarActionsTemplate } from "./postgres/console";
import { PostgresCredentialFormTemplate } from "./postgres/credential-form";
import { PostgresConnectorFormTemplate } from "./postgres/form";
import { PostgresConnectorRowActionsTemplate } from "./postgres/list-item";
import * as PostgresConnectorModel from "./postgres/model";
import { SSHConnectorConsoleTemplate, SSHConnectorToolbarActionsTemplate } from "./ssh/console";
import { SSHCredentialFormTemplate } from "./ssh/credential-form";
import { SSHCredentialRowActionsTemplate } from "./ssh/credential-row-actions";
import { SSHConnectorFormTemplate } from "./ssh/form";
import { SSHConnectorRowActionsTemplate } from "./ssh/list-item";
import * as SSHConnectorModel from "./ssh/model";
import { SSHConnectorOperationsTemplate } from "./ssh/operations";

export const connectorTemplates = Object.freeze({
  ssh: Object.freeze({
    metadata: getConnectorMetadata("ssh"),
    Console: SSHConnectorConsoleTemplate,
    CredentialForm: SSHCredentialFormTemplate,
    CredentialRowActions: SSHCredentialRowActionsTemplate,
    Form: SSHConnectorFormTemplate,
    model: SSHConnectorModel,
    Operations: SSHConnectorOperationsTemplate,
    RowActions: SSHConnectorRowActionsTemplate,
    ToolbarActions: SSHConnectorToolbarActionsTemplate,
  }),
  postgres: Object.freeze({
    metadata: getConnectorMetadata("postgres"),
    Console: PostgresConnectorConsoleTemplate,
    CredentialForm: PostgresCredentialFormTemplate,
    Form: PostgresConnectorFormTemplate,
    model: PostgresConnectorModel,
    RowActions: PostgresConnectorRowActionsTemplate,
    ToolbarActions: PostgresConnectorToolbarActionsTemplate,
  }),
});

export function getConnectorTemplate(kind) {
  return connectorTemplates[kind] || null;
}

export function getConnectorModel(kind) {
  return getConnectorTemplate(kind)?.model || null;
}

export function ConnectorTemplateNotFound({ kind, slot, as = "div", colSpan = 5 }) {
  const message = `Connector template not found: ${kind}/${slot}. Add frontend/src/connectors/templates/${kind}/${slot}.jsx and register it in connectorTemplates.`;
  if (as === "tr") {
    return (
      <tr>
        <td colSpan={colSpan} className="px-4 py-4">
          <Notice tone="bad">{message}</Notice>
        </td>
      </tr>
    );
  }
  return <Notice tone="bad">{message}</Notice>;
}
