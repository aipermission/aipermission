import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { RedisCredentialFormTemplate } from "../redis/credential-form";
import { RabbitMQCredentialFormTemplate } from "../rabbitmq/credential-form";
import { KafkaCredentialFormTemplate } from "../kafka/credential-form";
import type { UsernameCredentialForm } from "./connector-form-types";

const form: UsernameCredentialForm = { target_id: "1", profile_label: "reader", risk_label: "local", username: "service", password: "" };
const variants = [
  { Component: RedisCredentialFormTemplate, kind: "redis", label: "Redis", passwordRequired: false },
  { Component: RabbitMQCredentialFormTemplate, kind: "rabbitmq", label: "RabbitMQ", passwordRequired: true },
];

it.each(variants)("keeps $label creation and rotation rules explicit", async ({ Component, kind, passwordRequired }) => {
  const user = userEvent.setup();
  const onChange = vi.fn();
  const targets = [{ id: 1, name: "Local service", connector_kind: kind, config: { host: "service.test", port: 1234 } }];
  const props = { form, targets, state: { state: "idle" }, onChange, onSubmit: vi.fn() };
  const { rerender } = render(<Component {...props} />);
  expect(screen.getByLabelText("Password").hasAttribute("required")).toBe(passwordRequired);
  await user.type(screen.getByLabelText("Password"), "x");
  expect(onChange).toHaveBeenCalledWith({ ...form, password: "x" });
  rerender(<Component {...props} formMode="edit" />);
  expect(screen.getByLabelText("New password")).not.toBeRequired();
  expect(screen.getByLabelText("Connector target")).toBeDisabled();
  rerender(<Component {...props} targets={[]} />);
  expect(screen.getByRole("button")).toBeDisabled();
});

it("uses the selected Redis-compatible product when editing its credential", () => {
  render(
    <RedisCredentialFormTemplate
      form={form}
      formMode="edit"
      targets={[{ id: 1, name: "Cache", connector_kind: "redis", config: { server_family: "valkey" } }]}
      state={{ state: "error", error: "Validation failed" }}
      onChange={vi.fn()}
      onSubmit={vi.fn()}
    />,
  );
  expect(screen.getByRole("button", { name: "Save Valkey credential" })).toBeVisible();
  expect(screen.getByText("Validation failed")).toBeVisible();
});

it("requires a password when adding SASL to an unsecured Kafka credential", () => {
  const props = {
    form: { ...form, sasl_mechanism: "plain", existing_sasl_mechanism: "none" },
    targets: [{ id: 1, name: "Stream", connector_kind: "kafka", config: { bootstrap_brokers: ["broker.test:9092"] } }],
    state: { state: "idle" },
    onChange: vi.fn(),
    onSubmit: vi.fn(),
  };
  const { rerender } = render(<KafkaCredentialFormTemplate {...props} formMode="edit" />);
  expect(screen.getByLabelText("New password")).toBeRequired();
  expect(screen.getByText(/target must use TLS/)).toBeVisible();
  rerender(<KafkaCredentialFormTemplate {...props} form={{ ...props.form, sasl_mechanism: "none" }} />);
  expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
  rerender(<KafkaCredentialFormTemplate {...props} targets={[]} />);
  expect(screen.getByRole("button")).toBeDisabled();
});
