import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { verifyTransportProfileForm } from "../_shared/network-transport-form.test-support";
import { KubernetesConnectorFormTemplate } from "./form";

it("keeps Kubernetes wired to the shared transport profile contract", async () => {
  await verifyTransportProfileForm(KubernetesConnectorFormTemplate, { kubectl_command: "kubectl", kubeconfig_path: "" });
});

it("edits an explicit namespace scope", () => {
  const onChange = vi.fn();
  render(
    <KubernetesConnectorFormTemplate
      form={{
        name: "Scoped Kubernetes",
        connection_mode: "over_ssh",
        transport_target_ref: "ssh:4:8",
        kubectl_command: "kubectl",
        context: "",
        default_namespace: "",
        profile_label: "selected",
        risk_label: "production",
        scope_mode: "selected",
        namespaces: "production",
      }}
      targets={[]}
      onChange={onChange}
    />,
  );

  fireEvent.change(screen.getByLabelText("Namespaces"), { target: { value: "monitoring" } });
  expect(onChange).toHaveBeenCalledWith("namespaces", "monitoring");
});
