import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { verifyTransportProfileForm } from "../_shared/network-transport-form.test-support";
import { DockerConnectorFormTemplate } from "./form";

it("keeps Docker wired to the shared transport profile contract", async () => {
  await verifyTransportProfileForm(DockerConnectorFormTemplate, { docker_command: "docker" });
});

it("edits explicit container and pattern scopes", async () => {
  const user = userEvent.setup();
  const onChange = vi.fn();
  render(
    <DockerConnectorFormTemplate
      form={{
        name: "Scoped Docker",
        docker_command: "docker",
        profile_label: "selected",
        risk_label: "production",
        scope_mode: "selected",
        allowed_containers: "api",
        allowed_patterns: "worker-*",
        transport_target_ref: "ssh:4:8",
      }}
      targets={[]}
      onChange={onChange}
    />,
  );

  fireEvent.change(screen.getByLabelText("Allowed containers"), { target: { value: "web" } });
  await user.selectOptions(screen.getByLabelText("Container scope"), "all");

  expect(onChange).toHaveBeenCalledWith("allowed_containers", "web");
  expect(onChange).toHaveBeenCalledWith("scope_mode", "all");
});
