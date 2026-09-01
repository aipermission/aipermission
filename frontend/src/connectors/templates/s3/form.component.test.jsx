import { it } from "vitest";
import { verifyConnectionModeForm } from "../_shared/network-transport-form-test-support";
import { S3ConnectorFormTemplate } from "./form";

it("keeps S3 wired to the shared connection mode contract", async () => {
  await verifyConnectionModeForm(
    S3ConnectorFormTemplate,
    { scheme: "https", host: "s3.example.test", port: "443", region: "us-east-1", bucket: "artifacts", path_style: true },
    "For MinIO or S3-compatible storage running on the same Linux host",
  );
});
