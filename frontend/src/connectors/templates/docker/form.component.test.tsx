import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { verifyTransportProfileForm } from "../_shared/network-transport-form.test";
import { DockerConnectorFormTemplate } from "./form";
import { emptyForm } from "./model";

it("keeps Docker wired to the shared transport profile contract", async () => {
  await verifyTransportProfileForm(DockerConnectorFormTemplate, {
    ...emptyForm(),
    docker_command: "docker",
    scope_mode: "all",
    allowed_containers: "",
    allowed_patterns: "",
  });
});

it("edits explicit container and pattern scopes", async () => {
  const user = userEvent.setup();
  const onChange = vi.fn();
  render(
    <DockerConnectorFormTemplate
      form={{
        name: "Scoped Docker",
        connection_mode: "over_ssh",
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
  fireEvent.change(screen.getByLabelText("Allowed name patterns"), { target: { value: "api-*" } });
  await user.selectOptions(screen.getByLabelText("Container scope"), "all");

  expect(onChange).toHaveBeenCalledWith("allowed_containers", "web");
  expect(onChange).toHaveBeenCalledWith("allowed_patterns", "api-*");
  expect(onChange).toHaveBeenCalledWith("scope_mode", "all");
});
