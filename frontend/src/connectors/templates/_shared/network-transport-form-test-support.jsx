import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, vi } from "vitest";

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

export async function verifyConnectionModeForm(Component, form, directNotice) {
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
  await verifySSHProfileSelection(user, onChange);
}

export async function verifySSHProfileForm(Component, form) {
  const user = userEvent.setup();
  const onChange = vi.fn();
  const { container } = render(
    <Component form={{ ...baseForm, ...form, transport_target_ref: "ssh:4:8" }} targets={targets} onChange={onChange} />,
  );

  exerciseEditableFields(container);
  expect(onChange).toHaveBeenCalledWith("name", "changed");
  await verifySSHProfileSelection(user, onChange);
}

async function verifySSHProfileSelection(user, onChange) {
  const profile = screen.getByLabelText("SSH transport profile");
  expect(profile).toHaveValue("ssh:4:8");
  expect(screen.queryByRole("option", { name: /Other connector/ })).not.toBeInTheDocument();
  await user.selectOptions(profile, "ssh:4:8");
  expect(onChange).toHaveBeenCalledWith("transport_target_ref", "ssh:4:8");
}

function exerciseEditableFields(container) {
  for (const input of container.querySelectorAll("input:not(:disabled), textarea:not(:disabled)")) {
    if (input.type === "checkbox") {
      fireEvent.click(input);
    } else {
      fireEvent.change(input, { target: { value: input.type === "number" ? "42" : "changed" } });
    }
  }
  for (const select of container.querySelectorAll("select:not(:disabled)")) {
    const option = [...select.options].find((candidate) => !candidate.disabled && candidate.value !== select.value);
    if (option) fireEvent.change(select, { target: { value: option.value } });
  }
}
