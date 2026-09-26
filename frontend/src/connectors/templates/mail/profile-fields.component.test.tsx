import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { emptyForm } from "./model";
import { MailCredentialFields } from "./profile-fields";

it("requires new protocol credentials on creation but preserves encrypted credentials during edits", () => {
  const form = { ...emptyForm(), smtp_auth_mode: "separate" };
  const { rerender } = render(<MailCredentialFields form={form} editing={false} onChange={vi.fn()} />);
  for (const name of ["IMAP username", "IMAP password or app password", "SMTP username", "SMTP password or app password"]) {
    expect(screen.getByLabelText(name)).toBeRequired();
  }
  rerender(<MailCredentialFields form={form} editing onChange={vi.fn()} />);
  for (const name of ["IMAP username", "IMAP password or app password", "SMTP username", "SMTP password or app password"]) {
    expect(screen.getByLabelText(name)).not.toBeRequired();
  }
});

it("routes folder policies and SMTP mode through their named form fields", async () => {
  const user = userEvent.setup();
  const form = emptyForm();
  const onChange = vi.fn();
  render(<MailCredentialFields form={form} editing={false} onChange={onChange} />);
  await user.selectOptions(screen.getByLabelText("SMTP authentication"), "separate");
  expect(onChange).toHaveBeenCalledWith("smtp_auth_mode", "separate");
  fireEvent.change(screen.getByLabelText("Move destinations"), { target: { value: "Archive" } });
  expect(onChange).toHaveBeenLastCalledWith("allowed_mutation_destination_folders", "Archive");
  fireEvent.change(screen.getByLabelText("Sent folder"), { target: { value: "Sent" } });
  expect(onChange).toHaveBeenLastCalledWith("sent_folder", "Sent");
});
