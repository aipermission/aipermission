import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { verifyConnectionModeForm } from "../_shared/network-transport-form.test";
import { RedisConnectorFormTemplate } from "./form";
import { emptyForm } from "./model";

it("keeps Redis wired to the shared connection mode contract", async () => {
  await verifyConnectionModeForm(
    RedisConnectorFormTemplate,
    {
      ...emptyForm(),
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

it("wires explicit stored password removal in the connector editor", () => {
  const onChange = vi.fn();
  const form = emptyForm();
  const { rerender } = render(<RedisConnectorFormTemplate form={form} mode="edit" onChange={onChange} />);
  fireEvent.click(screen.getByRole("checkbox", { name: "Remove stored password" }));
  expect(onChange).toHaveBeenCalledWith("clear_password", true);
  rerender(<RedisConnectorFormTemplate form={{ ...form, clear_password: true }} mode="edit" onChange={onChange} />);
  expect(screen.getByLabelText("Password")).toBeDisabled();
  fireEvent.click(screen.getByRole("checkbox", { name: "Remove stored password" }));
  expect(onChange).toHaveBeenCalledWith("clear_password", false);
});
