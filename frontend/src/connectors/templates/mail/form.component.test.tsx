import { expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { verifyConnectionModeForm } from "../_shared/network-transport-form.test";
import { MailConnectorFormTemplate } from "./form";
import { emptyForm } from "./model";

it("keeps Mail wired to the shared connection mode contract", async () => {
  await verifyConnectionModeForm(
    MailConnectorFormTemplate,
    {
      ...emptyForm(),
      imap_host: "imap.example.test",
      imap_port: "993",
      imap_tls_mode: "implicit_tls",
      smtp_host: "smtp.example.test",
      smtp_port: "465",
      smtp_tls_mode: "implicit_tls",
      allowed_recipient_domains: "",
    },
    "The local gateway connects to both mail endpoints.",
  );
});

it("labels each mail endpoint input independently of its ping button", () => {
  render(<MailConnectorFormTemplate form={emptyForm()} onChange={vi.fn()} />);
  const imap = screen.getByRole("textbox", { name: "IMAP host" });
  const smtp = screen.getByRole("textbox", { name: "SMTP host" });
  expect(screen.getByLabelText("IMAP host")).toBe(imap);
  expect(screen.getByLabelText("SMTP host")).toBe(smtp);
  expect(imap.id).not.toBe(smtp.id);
  expect(screen.getAllByRole("button", { name: "Ping host" })).toHaveLength(2);
});
