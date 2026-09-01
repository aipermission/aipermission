import { it } from "vitest";
import { verifySSHProfileForm } from "../_shared/network-transport-form-test-support";
import { KubernetesConnectorFormTemplate } from "./form";

it("keeps Kubernetes wired to the shared SSH profile contract", async () => {
  await verifySSHProfileForm(KubernetesConnectorFormTemplate, { kubectl_command: "kubectl", kubeconfig_path: "" });
});
