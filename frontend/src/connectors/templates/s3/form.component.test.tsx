import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { verifyConnectionModeForm } from "../_shared/network-transport-form.test";
import { S3ConnectorFormTemplate } from "./form";
import { S3CredentialFormTemplate } from "./credential-form";
import { emptyForm, formFromTarget, s3TargetConfigFromForm } from "./model";

it("keeps S3 wired to the shared connection mode contract", async () => {
  await verifyConnectionModeForm(
    S3ConnectorFormTemplate,
    {
      scheme: "https",
      host: "s3.example.test",
      port: "443",
      region: "us-east-1",
      bucket: "artifacts",
      path_style: true,
      trust_conditional_requests: false,
    },
    "For MinIO or S3-compatible storage running on the same Linux host",
  );
});

it("round-trips and wires verified conditional request trust", () => {
  expect(emptyForm().trust_conditional_requests).toBe(false);
  const restored = formFromTarget({
    target: {
      name: "objects",
      config: { host: "s3.example.test", bucket: "artifacts", trust_conditional_requests: true },
      profiles: [],
    },
    profile: null,
  });
  expect(restored.trust_conditional_requests).toBe(true);
  expect(s3TargetConfigFromForm(restored).trust_conditional_requests).toBe(true);

  const onChange = vi.fn();
  render(<S3ConnectorFormTemplate form={{ ...emptyForm(), trust_conditional_requests: false }} onChange={onChange} mode="create" />);
  fireEvent.click(screen.getByRole("checkbox", { name: /Verified conditional requests/i }));
  expect(onChange).toHaveBeenCalledWith("trust_conditional_requests", true);
});

it("limits credential selection to S3 targets and updates the access key", () => {
  const onChange = vi.fn();
  const form = {
    target_id: "1",
    profile_label: "default",
    risk_label: "storage",
    access_key_id: "old-key",
    secret_access_key: "",
    session_token: "",
  };
  render(
    <S3CredentialFormTemplate
      targets={[
        { id: 1, name: "Objects", connector_kind: "s3", config: { host: "s3.example.test" } },
        { id: 2, name: "Server", connector_kind: "ssh" },
      ]}
      form={form}
      state={{ state: "idle" }}
      onChange={onChange}
      onSubmit={vi.fn()}
    />,
  );
  expect(screen.getByRole("option", { name: /Objects/ })).toBeInTheDocument();
  expect(screen.queryByRole("option", { name: /Server/ })).not.toBeInTheDocument();
  fireEvent.change(screen.getByRole("textbox", { name: "Access key ID" }), { target: { value: "new-key" } });
  expect(onChange).toHaveBeenCalledWith({ ...form, access_key_id: "new-key" });
});
