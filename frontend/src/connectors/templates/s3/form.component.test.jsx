import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { verifyConnectionModeForm } from "../_shared/network-transport-form-test-support";
import { S3ConnectorFormTemplate } from "./form";
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
  });
  expect(restored.trust_conditional_requests).toBe(true);
  expect(s3TargetConfigFromForm(restored).trust_conditional_requests).toBe(true);

  const onChange = vi.fn();
  render(<S3ConnectorFormTemplate form={{ ...emptyForm(), trust_conditional_requests: false }} onChange={onChange} mode="create" />);
  fireEvent.click(screen.getByRole("checkbox", { name: /Verified conditional requests/i }));
  expect(onChange).toHaveBeenCalledWith("trust_conditional_requests", true);
});
