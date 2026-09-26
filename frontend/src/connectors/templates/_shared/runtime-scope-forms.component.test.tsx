import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { DockerCredentialFormTemplate } from "../docker/credential-form";
import { KubernetesCredentialFormTemplate } from "../kubernetes/credential-form";

const profile = { target_id: "1", profile_label: "selected", risk_label: "local" };

it("edits container and pattern allowlists without permitting credential reassignment", async () => {
  const user = userEvent.setup();
  const onChange = vi.fn();
  const form = { ...profile, scope_mode: "selected", allowed_containers: "api", allowed_patterns: "worker-*" };
  const props = {
    form,
    targets: [{ id: 1, name: "Local runtime", connector_kind: "docker" }],
    state: { state: "idle" },
    onChange,
    onSubmit: vi.fn(),
  };
  const { rerender } = render(<DockerCredentialFormTemplate {...props} formMode="edit" />);
  expect(screen.getByLabelText("Connector target")).toBeDisabled();
  await user.type(screen.getByLabelText("Allowed containers"), "x");
  expect(onChange).toHaveBeenCalledWith({ ...form, allowed_containers: "apix" });
  await user.selectOptions(screen.getByLabelText("Container scope"), "all");
  expect(onChange).toHaveBeenCalledWith({ ...form, scope_mode: "all" });
  rerender(<DockerCredentialFormTemplate {...props} form={{ ...form, scope_mode: "all" }} />);
  expect(screen.queryByLabelText("Allowed containers")).not.toBeInTheDocument();
  rerender(<DockerCredentialFormTemplate {...props} targets={[]} />);
  expect(screen.getByRole("button")).toBeDisabled();
});

it("edits namespaces within the selected Kubernetes credential", async () => {
  const user = userEvent.setup();
  const onChange = vi.fn();
  const form = { ...profile, scope_mode: "selected", namespaces: "production" };
  const props = {
    form,
    targets: [{ id: 1, name: "Local cluster", connector_kind: "kubernetes" }],
    state: { state: "error", error: "Scope rejected" },
    onChange,
    onSubmit: vi.fn(),
  };
  const { rerender } = render(<KubernetesCredentialFormTemplate {...props} formMode="edit" />);
  expect(screen.getByLabelText("Connector target")).toBeDisabled();
  expect(screen.getByText("Scope rejected")).toBeVisible();
  await user.type(screen.getByLabelText("Namespaces"), "x");
  expect(onChange).toHaveBeenCalledWith({ ...form, namespaces: "productionx" });
  await user.selectOptions(screen.getByLabelText("Namespace scope"), "all");
  expect(onChange).toHaveBeenCalledWith({ ...form, scope_mode: "all" });
  rerender(<KubernetesCredentialFormTemplate {...props} form={{ ...form, scope_mode: "all" }} />);
  expect(screen.queryByLabelText("Namespaces")).not.toBeInTheDocument();
  rerender(<KubernetesCredentialFormTemplate {...props} targets={[]} />);
  expect(screen.getByRole("button")).toBeDisabled();
});
