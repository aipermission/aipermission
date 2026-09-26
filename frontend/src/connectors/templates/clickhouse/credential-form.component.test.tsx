import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import type { ComponentProps } from "react";
import { ClickHouseCredentialFormTemplate } from "./credential-form";

it("allows empty passwords while preserving profile metadata when rotating a secret", async () => {
  const user = userEvent.setup();
  const props: ComponentProps<typeof ClickHouseCredentialFormTemplate> = {
    targets: [{ id: 1, name: "Analytics", connector_kind: "clickhouse" }],
    formMode: "edit",
    form: { target_id: "1", profile_label: "reader", risk_label: "analytics", username: "reader", password: "" },
    state: { state: "idle" },
    onChange: vi.fn(),
    onSubmit: vi.fn((event) => event.preventDefault()),
  };
  render(<ClickHouseCredentialFormTemplate {...props} />);

  const password = screen.getByLabelText("New password");
  expect(password).not.toBeRequired();
  expect(screen.getByLabelText("Connector target")).toBeDisabled();
  await user.type(password, "x");
  expect(props.onChange).toHaveBeenCalledWith({ ...props.form, password: "x" });
  await user.click(screen.getByRole("button", { name: "Save ClickHouse credential" }));
  expect(props.onSubmit).toHaveBeenCalledOnce();
});
