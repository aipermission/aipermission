import { it } from "vitest";
import { verifyTransportProfileForm } from "../_shared/network-transport-form.test-support";
import { KubernetesConnectorFormTemplate } from "./form";

it("keeps Kubernetes wired to the shared transport profile contract", async () => {
  await verifyTransportProfileForm(KubernetesConnectorFormTemplate, { kubectl_command: "kubectl", kubeconfig_path: "" });
});
