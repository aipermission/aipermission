import { Notice } from "../../components/ui/notice";
import { connectorTemplateMetadata, getConnectorMetadata } from "./catalog";
import { uniqueNetworkTransportDescriptors } from "./_shared/network-transport-contract";
import { assertConnectorTemplate } from "./template-registration";
import { getConsoleSessionRecovery } from "./console-recovery-registry";
import { capturedConsoleSlots } from "./_shared/console-template";
import { registerNativeFamilies } from "./native-family-registry";
import type { ConnectorMetadata } from "./catalog";
import type { ConsoleTemplateSlots } from "../../components/console/console-connector-view-types";

type RegisteredConsoleTemplate = Readonly<ConsoleTemplateSlots & { metadata: Readonly<ConnectorMetadata> }>;
const templateModules = import.meta.glob("./*/index.ts", { eager: true, import: "default" });

export const connectorTemplates = registerConnectorTemplates(templateModules);
uniqueNetworkTransportDescriptors(Object.entries(connectorTemplateMetadata));

export function registerConnectorTemplates(modules: Record<string, unknown>) {
  return registerNativeFamilies<RegisteredConsoleTemplate>(
    modules,
    Object.keys(connectorTemplateMetadata),
    "Connector template",
    (kind, native) => {
      const slots = capturedConsoleSlots(native);
      const metadata = getConnectorMetadata(kind);
      if (!slots || native === null || typeof native !== "object") throw new Error(`Connector ${kind} must use defineConsoleTemplate.`);
      if (!metadata) throw new Error(`Connector ${kind} metadata is missing.`);
      assertConnectorTemplate(kind, { ...native, metadata });
      return Object.freeze({ ...slots, metadata, Operations: getConsoleSessionRecovery(kind)?.Operations });
    },
  );
}

export function getConnectorTemplate(kind: string): RegisteredConsoleTemplate | null {
  return Object.hasOwn(connectorTemplates, kind) ? connectorTemplates[kind] : null;
}

export function ConnectorTemplateNotFound({
  kind,
  slot,
  as = "div",
  colSpan = 6,
}: {
  kind: string;
  slot: string;
  as?: "div" | "tr";
  colSpan?: number;
}) {
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
