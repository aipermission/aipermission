import { Notice } from "../../components/ui/notice";
import { connectorTemplateMetadata, getConnectorMetadata } from "./catalog";
import { uniqueNetworkTransportDescriptors } from "./_shared/network-transport-contract";
import { assertConnectorTemplate, connectorKindFromPath } from "./template-registration";
import { getConsoleSessionRecovery } from "./console-recovery-registry";

const templateModules = import.meta.glob("./*/index.ts", { eager: true });

export const connectorTemplates = Object.freeze(
  Object.fromEntries(
    Object.entries(templateModules)
      .map(([path, module]) => {
        const kind = connectorKindFromPath(path);
        const template = module.default || module.template || {};
        return [
          kind,
          Object.freeze({
            ...template,
            metadata: getConnectorMetadata(kind),
            Operations: getConsoleSessionRecovery(kind)?.Operations,
          }),
        ];
      })
      .sort(([left], [right]) => left.localeCompare(right)),
  ),
);

assertConnectorTemplateRegistration();

export function getConnectorTemplate(kind) {
  return connectorTemplates[kind] || null;
}

export function getConnectorModel(kind) {
  return getConnectorTemplate(kind)?.model || null;
}

export function ConnectorTemplateNotFound({ kind, slot, as = "div", colSpan = 6 }) {
  const message = `Connector template not found: ${kind}/${slot}. Add frontend/src/connectors/templates/${kind}/index.ts and export the ${slot} slot.`;
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

function assertConnectorTemplateRegistration() {
  const catalogKinds = Object.keys(connectorTemplateMetadata).sort();
  const registryKinds = Object.keys(connectorTemplates).sort();
  if (catalogKinds.length !== registryKinds.length || catalogKinds.some((kind, index) => kind !== registryKinds[index])) {
    throw new Error(`Connector template catalog/registry mismatch. catalog=${catalogKinds.join(",")} registry=${registryKinds.join(",")}`);
  }
  for (const kind of registryKinds) {
    assertConnectorTemplate(kind, connectorTemplates[kind]);
  }
  uniqueNetworkTransportDescriptors(Object.entries(connectorTemplateMetadata));
}
