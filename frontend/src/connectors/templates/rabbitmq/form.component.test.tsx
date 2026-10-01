import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { verifyConnectionModeForm } from "../_shared/network-transport-form.test";
import { RabbitMQConnectorFormTemplate } from "./form";
import { emptyForm } from "./model";

it("keeps RabbitMQ wired to the shared connection mode contract", async () => {
  await verifyConnectionModeForm(
    RabbitMQConnectorFormTemplate,
    { ...emptyForm(), scheme: "https", host: "rabbit.example.test", port: "15672", vhost: "/", username: "operator", password: "" },
    "For RabbitMQ Management running on the same Linux host",
  );
});

it.each([" tenant ", " \t ", ""])("forwards the exact default vhost from the target form: %j", (vhost) => {
  const onChange = vi.fn();
  render(<RabbitMQConnectorFormTemplate form={{ ...emptyForm(), vhost }} onChange={onChange} />);
  const input = screen.getByLabelText("Default vhost");
  expect(input).toHaveValue(vhost);
  const next = `${vhost} `;
  fireEvent.change(input, { target: { value: next } });
  expect(onChange).toHaveBeenCalledExactlyOnceWith("vhost", next);
});
