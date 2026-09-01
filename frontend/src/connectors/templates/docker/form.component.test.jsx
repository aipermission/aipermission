import { it } from "vitest";
import { verifySSHProfileForm } from "../_shared/network-transport-form-test-support";
import { DockerConnectorFormTemplate } from "./form";

it("keeps Docker wired to the shared SSH profile contract", async () => {
  await verifySSHProfileForm(DockerConnectorFormTemplate, { docker_command: "docker" });
});
