import { defineCredentialFamily } from "../_shared/credential-family-registration";
import { credentialDisplayRow } from "../_shared/credential-display-row";
import { optionalConsoleText } from "../_shared/console-target-config";
import type { InventoryTarget } from "../../../lib/gateway-contracts/connector-inventory-contract";
import { KubernetesCredentialFormTemplate } from "./credential-form";
import * as model from "./model";

type Target = ReturnType<typeof kubernetesCredentialTargets>[number];
type Profile = Target["profiles"][number];
type State = ReturnType<typeof model.emptyCredentialState>;
type Row = ReturnType<typeof model.credentialRows<Profile, Target>>[number];

export const kubernetesCredentialFamily = defineCredentialFamily<State, Row, Target, "create" | "update">({
  kind: "kubernetes",
  label: "Kubernetes",
  decodeTargets: kubernetesCredentialTargets,
  emptyState: model.emptyCredentialState,
  model,
  rows: ({ targets }) => model.credentialRows<Profile, Target>({ targets }),
  displayRow: credentialDisplayRow,
  renderForm: ({ editor, targets }) => (
    <KubernetesCredentialFormTemplate
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

export function kubernetesCredentialTargets(targets: readonly InventoryTarget[]) {
  return targets
    .filter((target) => target.connector_kind === "kubernetes")
    .map((target) => {
      const config = target.config || {};
      return {
        ...target,
        config: {
          ...config,
          connection_mode: optionalConsoleText(config.connection_mode, "Kubernetes", "connection_mode"),
          transport_target_ref: optionalConsoleText(config.transport_target_ref, "Kubernetes", "transport_target_ref"),
          kubectl_command: optionalConsoleText(config.kubectl_command, "Kubernetes", "kubectl_command"),
          context: optionalConsoleText(config.context, "Kubernetes", "context"),
          default_namespace: optionalConsoleText(config.default_namespace, "Kubernetes", "default_namespace"),
        },
        profiles: (target.profiles || []).map((profile) => ({
          ...profile,
          public: {
            ...profile.public,
            scope_mode: optionalConsoleText(profile.public?.scope_mode, "Kubernetes", "scope_mode"),
            namespaces: optionalConsoleText(profile.public?.namespaces, "Kubernetes", "namespaces"),
          },
        })),
      };
    });
}
