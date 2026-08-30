import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { CredentialProfileFields } from "./credential-profile-fields";

const targets = [
  { id: 1, name: "Primary" },
  { id: 2, name: "Replica" },
];

describe("CredentialProfileFields", () => {
  it("updates shared metadata without owning connector-specific labels", async () => {
    const user = userEvent.setup();
    const form = { target_id: "1", profile_label: "admin", risk_label: "write" };
    const onChange = vi.fn();
    render(
      <CredentialProfileFields
        targets={targets}
        form={form}
        editing={false}
        onChange={onChange}
        targetPlaceholder="Select target"
        targetOptionLabel={(target) => `${target.name} endpoint`}
      />,
    );

    await user.selectOptions(screen.getByRole("combobox", { name: "Connector target" }), "2");
    expect(onChange).toHaveBeenCalledWith({ ...form, target_id: "2" });
    expect(screen.getByRole("option", { name: "Replica endpoint" })).toBeVisible();
  });

  it("locks target identity during credential edits", () => {
    render(
      <CredentialProfileFields
        targets={targets}
        form={{ target_id: "1", profile_label: "admin", risk_label: "" }}
        editing
        onChange={vi.fn()}
        targetPlaceholder="Select target"
        targetOptionLabel={(target) => target.name}
      />,
    );

    expect(screen.getByRole("combobox", { name: "Connector target" })).toBeDisabled();
    expect(screen.getByRole("textbox", { name: "Profile label" })).toBeEnabled();
    expect(screen.getByRole("textbox", { name: "Risk label" })).toBeEnabled();
  });
});
