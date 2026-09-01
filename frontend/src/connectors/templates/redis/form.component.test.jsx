import { it } from "vitest";
import { verifyConnectionModeForm } from "../_shared/network-transport-form-test-support";
import { RedisConnectorFormTemplate } from "./form";

it("keeps Redis wired to the shared connection mode contract", async () => {
  await verifyConnectionModeForm(
    RedisConnectorFormTemplate,
    {
      server_family: "redis",
      host: "redis.example.test",
      port: "6379",
      database: "0",
      tls_mode: "verify_full",
      username: "",
      password: "",
    },
    "For Redis running on the same Linux host",
  );
});
