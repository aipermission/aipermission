import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { renderConnectorFamily } from "../../../test/connector-family-host";
import { captureConnectorFamily } from "../../editor/capture-connector-family";
import { mailConnectorFamily } from "./connector-family";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn(), apiGet: vi.fn() }));
const family = mailConnectorFamily.create(captureConnectorFamily);
const publicProfile = {
  mailbox_address: "support@example.com",
  display_name: "",
  reply_to: "",
  imap_enabled: true,
  smtp_auth_mode: "reuse_imap",
  allowed_read_folders: ["INBOX", "Sent"],
  allowed_mutation_source_folders: ["INBOX"],
  allowed_mutation_destination_folders: [],
  sent_folder: "Sent",
  archive_folder: "",
  trash_folder: "",
};
const profile = inventoryProfileFixture({ connector_kind: "mail", kind: "password", label: "support", public: publicProfile });
const config = {
  connection_mode: "direct",
  transport_target_ref: "",
  imap_host: "imap.example.com",
  imap_port: 993,
  imap_tls_mode: "implicit_tls",
  smtp_host: "smtp.example.com",
  smtp_port: 465,
  smtp_tls_mode: "implicit_tls",
  allowed_recipient_domains: [],
};
const target = inventoryTargetFixture({ connector_kind: "mail", name: "My mailbox", config, profiles: [profile] });
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(apiPost)
    .mockReset()
    .mockResolvedValue({ id: 3, profiles: [{ id: 11 }] });
  vi.mocked(apiPut)
    .mockReset()
    .mockResolvedValue({ id: 3, profiles: [{ id: 11 }] });
  vi.mocked(apiDelete).mockReset().mockResolvedValue({});
});

it("creates a Mail connector with native protocol policy and encrypted login fields", async () => {
  const user = userEvent.setup();
  const host = renderConnectorFamily(family);
  act(() => host.commands().openCreate());
  await user.type(screen.getByLabelText("Mailbox address"), "support@example.com");
  await user.type(screen.getByLabelText("IMAP username"), "support");
  await user.type(screen.getByLabelText("IMAP password or app password"), "test-password");
  await user.selectOptions(screen.getByLabelText("SMTP authentication"), "reuse_imap");
  await user.click(screen.getByRole("button", { name: "Create connector" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/with-profile", {
    target: { connector_kind: "mail", name: "mailbox", project_id: 7, config },
    profile: {
      kind: "password",
      label: "mailbox",
      risk_label: "mailbox access",
      public: { ...publicProfile, allowed_read_folders: ["INBOX"], sent_folder: "" },
      secret: { imap_username: "support", imap_password: "test-password" },
    },
  });
});

it("edits the selected Mail profile without serializing untouched login secrets", async () => {
  const user = userEvent.setup();
  const host = renderConnectorFamily(family, { targets: [target] });
  act(() => host.commands().openEdit(target, profile));
  expect(screen.getByLabelText("Mailbox address")).toHaveValue("support@example.com");
  expect(screen.getByLabelText("IMAP username")).toHaveValue("");
  await user.type(screen.getByLabelText("Display name"), "Support");
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}/with-profile/${profile.id}`, {
    target: { name: "My mailbox", project_id: 7, config },
    profile: { kind: "password", label: "support", public: { ...publicProfile, display_name: "Support" }, risk_label: "mailbox access" },
  });
});

it("disables saving when neither native Mail protocol is enabled", async () => {
  const user = userEvent.setup();
  const host = renderConnectorFamily(family);
  act(() => host.commands().openCreate());
  await user.selectOptions(screen.getByLabelText("SMTP authentication"), "reuse_imap");
  await user.click(screen.getByLabelText("Enable IMAP mailbox access"));
  expect(screen.getByLabelText("SMTP authentication")).toHaveValue("disabled");
  expect(screen.getByRole("button", { name: "Create connector" })).toBeDisabled();
  expect(apiPost).not.toHaveBeenCalled();
});

it("tests a Mail profile and removes only local mailbox configuration", async () => {
  const user = userEvent.setup();
  const host = renderConnectorFamily(family, { targets: [target] });
  vi.mocked(apiPost).mockResolvedValueOnce({ ok: true });
  await act(async () => {
    expect(await host.commands().test(target, profile)).toBe(true);
  });
  expect(apiPost).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}/profiles/${profile.id}/test`, {});
  act(() => host.commands().requestDelete(target));
  expect(screen.getByText(/does not delete mailbox messages/)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Delete connector" }));
  await waitFor(() => expect(apiDelete).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}`));
});
