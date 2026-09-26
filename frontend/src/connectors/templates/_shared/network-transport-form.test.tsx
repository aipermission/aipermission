import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, vi } from "vitest";
import type { ComponentType } from "react";
import type { ConnectorFormProps } from "./connector-form-types";

const targets = [
  {
    connector_kind: "ssh",
    id: 4,
    name: "SSH transport",
    config: { host: "ops.example.test", port: 22 },
    profiles: [{ id: 8, label: "root" }],
  },
  {
    connector_kind: "database",
    id: 9,
    name: "Other connector",
    profiles: [{ id: 2, label: "readonly" }],
  },
];

const baseForm = {
  name: "My Connector",
  project_id: 1,
  connection_mode: "direct",
  transport_target_ref: "",
  profile_label: "default",
  risk_label: "",
};

export async function verifyConnectionModeForm<Form, Value = string>(
  Component: ComponentType<ConnectorFormProps<typeof baseForm & Form, Value>>,
  form: Form,
  directNotice: string,
) {
  const user = userEvent.setup();
  const onChange = vi.fn();
  const { container, rerender } = render(<Component form={{ ...baseForm, ...form }} targets={targets} onChange={onChange} />);

  expect(screen.getByText(new RegExp(directNotice))).toBeVisible();
  exerciseEditableFields(container);
  expect(onChange).toHaveBeenCalledWith("name", "changed");
  await user.selectOptions(screen.getByLabelText("Connection mode"), "over_ssh");
  expect(onChange).toHaveBeenCalledWith("connection_mode", "over_ssh");

  rerender(
    <Component
      form={{ ...baseForm, ...form, connection_mode: "over_ssh", transport_target_ref: "ssh:4:8" }}
      targets={targets}
      onChange={onChange}
    />,
  );
  await verifyTransportProfileSelection(user, onChange, "SSH connector profile");
}

export async function verifyTransportProfileForm<Form, Value = string>(
  Component: ComponentType<ConnectorFormProps<typeof baseForm & Form, Value>>,
  form: Form,
) {
  const user = userEvent.setup();
  const onChange = vi.fn();
  const { container } = render(
    <Component
      form={{ ...baseForm, ...form, connection_mode: "over_ssh", transport_target_ref: "ssh:4:8" }}
      targets={targets}
      onChange={onChange}
    />,
  );

  exerciseEditableFields(container);
  expect(onChange).toHaveBeenCalledWith("name", "changed");
  await verifyTransportProfileSelection(user, onChange, "Transport profile");
}

async function verifyTransportProfileSelection(
  user: ReturnType<typeof userEvent.setup>,
  onChange: ReturnType<typeof vi.fn>,
  label: string,
) {
  const profile = screen.getByLabelText(label);
  expect(profile).toHaveValue("ssh:4:8");
  expect(screen.queryByRole("option", { name: /Other connector/ })).not.toBeInTheDocument();
  await user.selectOptions(profile, "ssh:4:8");
  expect(onChange).toHaveBeenCalledWith("transport_target_ref", "ssh:4:8");
}

function exerciseEditableFields(container: HTMLElement) {
  for (const input of Array.from(
    container.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>("input:not(:disabled), textarea:not(:disabled)"),
  )) {
    if (input.type === "checkbox") {
      fireEvent.click(input);
    } else {
      fireEvent.change(input, { target: { value: input.type === "number" ? "42" : "changed" } });
    }
  }
  for (const select of Array.from(container.querySelectorAll<HTMLSelectElement>("select:not(:disabled)"))) {
    const option = Array.from(select.options).find((candidate) => !candidate.disabled && candidate.value !== select.value);
    if (option) fireEvent.change(select, { target: { value: option.value } });
  }
}
