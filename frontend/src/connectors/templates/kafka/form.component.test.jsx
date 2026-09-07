import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { verifyConnectionModeForm } from "../_shared/network-transport-form.test-support";
import { KafkaConnectorFormTemplate } from "./form";

it("keeps Kafka wired to the shared connection mode contract", async () => {
  await verifyConnectionModeForm(
    KafkaConnectorFormTemplate,
    { bootstrap_brokers: "broker:9092", server_family: "kafka", sasl_mechanism: "none", tls_enabled: false },
    "The gateway must reach bootstrap and advertised broker addresses.",
  );
});

it("renders TLS and edit-mode SASL credential branches", async () => {
  const user = userEvent.setup();
  const onChange = vi.fn();
  render(
    <KafkaConnectorFormTemplate
      mode="edit"
      form={{
        name: "Event stream",
        server_family: "redpanda",
        connection_mode: "direct",
        transport_target_ref: "",
        project_id: 1,
        bootstrap_brokers: "[2001:db8::1]:9093",
        tls_enabled: true,
        tls_server_name: "broker.example.test",
        tls_ca_pem: "CA",
        profile_label: "writer",
        risk_label: "production",
        sasl_mechanism: "plain",
        existing_sasl_mechanism: "plain",
        username: "service",
        password: "",
      }}
      onChange={onChange}
    />,
  );

  expect(screen.getByLabelText("Custom CA certificate")).toHaveValue("CA");
  expect(screen.getByLabelText("New password")).not.toBeRequired();
  await user.selectOptions(screen.getByLabelText("TLS"), "disabled");
  expect(onChange).toHaveBeenCalledWith("tls_enabled", false);
});

it("requires an explicit exception for PLAIN SASL without TLS", async () => {
  const user = userEvent.setup();
  const onChange = vi.fn();
  render(
    <KafkaConnectorFormTemplate
      form={{
        name: "Local stream",
        server_family: "kafka",
        connection_mode: "direct",
        transport_target_ref: "",
        project_id: 1,
        bootstrap_brokers: "broker",
        tls_enabled: false,
        tls_server_name: "",
        tls_ca_pem: "",
        profile_label: "writer",
        risk_label: "",
        sasl_mechanism: "plain",
        allow_insecure_plain_sasl: false,
        username: "service",
        password: "secret",
      }}
      onChange={onChange}
    />,
  );

  const insecurePlain = screen.getByText("PLAIN without TLS").closest("label").querySelector("select");
  await user.selectOptions(insecurePlain, "allowed");
  expect(onChange).toHaveBeenCalledWith("allow_insecure_plain_sasl", true);
});
