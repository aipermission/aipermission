import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { MailCredentialFormTemplate } from "./credential-form";
import { credentialFormProps, emptyCredentialState } from "./model";

const targets = [{ id: 4, connector_kind: "mail", name: "Support", config: { imap_host: "imap.example.test", imap_port: 993 } }];

it("disabling IMAP composes the dependent SMTP mode update", async () => {
  const user = userEvent.setup();
  const onSubmit = vi.fn((event) => event.preventDefault());

  function Harness() {
    const [formState, setFormState] = useState({
      form: {
        ...emptyCredentialState({ targets }).form,
        imap_enabled: true,
        smtp_auth_mode: "reuse_imap",
      },
      auxiliary: "preserved",
    });
    const props = credentialFormProps({
      targets,
      formState,
      setFormState,
      formMode: "edit",
      state: { state: "idle", error: "" },
      onSubmit,
    });
    return (
      <>
        <MailCredentialFormTemplate {...props} targets={targets} />
        <output>{formState.auxiliary}</output>
      </>
    );
  }

  render(<Harness />);
  await user.click(screen.getByRole("checkbox", { name: "Enable IMAP mailbox access" }));

  expect(screen.getByRole("checkbox", { name: "Enable IMAP mailbox access" })).not.toBeChecked();
  expect(screen.getByRole("combobox", { name: "SMTP authentication" })).toHaveValue("disabled");
  expect(screen.getByRole("button", { name: "Save mail credential" })).toBeDisabled();
  expect(screen.getByText("preserved")).toBeVisible();
});
