import { it } from "vitest";
import { verifyConnectionModeForm } from "../_shared/network-transport-form-test-support";
import { RabbitMQConnectorFormTemplate } from "./form";

it("keeps RabbitMQ wired to the shared connection mode contract", async () => {
  await verifyConnectionModeForm(
    RabbitMQConnectorFormTemplate,
    { scheme: "https", host: "rabbit.example.test", port: "15672", vhost: "/", username: "operator", password: "" },
    "For RabbitMQ Management running on the same Linux host",
  );
});
