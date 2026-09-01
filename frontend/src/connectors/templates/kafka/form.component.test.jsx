import { it } from "vitest";
import { verifyConnectionModeForm } from "../_shared/network-transport-form-test-support";
import { KafkaConnectorFormTemplate } from "./form";

it("keeps Kafka wired to the shared connection mode contract", async () => {
  await verifyConnectionModeForm(
    KafkaConnectorFormTemplate,
    { bootstrap_brokers: "broker:9092", server_family: "kafka", sasl_mechanism: "none", tls_enabled: false },
    "The gateway must reach bootstrap and advertised broker addresses.",
  );
});
