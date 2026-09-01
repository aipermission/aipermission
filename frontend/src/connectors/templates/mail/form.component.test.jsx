import { it } from "vitest";
import { verifyConnectionModeForm } from "../_shared/network-transport-form-test-support";
import { MailConnectorFormTemplate } from "./form";

it("keeps Mail wired to the shared connection mode contract", async () => {
  await verifyConnectionModeForm(
    MailConnectorFormTemplate,
    {
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
