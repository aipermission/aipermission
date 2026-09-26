import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { renderCredentialFamily } from "../../../test/render-credential-family";
import { captureCredentialFamily } from "../../editor/capture-credential-family";
import { mailCredentialFamily, mailCredentialTargets } from "./credential-family";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn() }));
const familyTemplate = mailCredentialFamily.create(captureCredentialFamily);
const target = inventoryTargetFixture({
  connector_kind: "mail",
  name: "Test mailbox",
  config: { imap_host: "imap.example.test", imap_port: 993, smtp_host: "smtp.example.test", smtp_port: 465 },
  profiles: [
    inventoryProfileFixture({
      connector_kind: "mail",
      kind: "password",
      label: "Support",
      public: {
        mailbox_address: "support@example.test",
        imap_enabled: true,
        smtp_auth_mode: "reuse_imap",
        allowed_read_folders: ["INBOX", "Sent"],
      },
    }),
  ],
});
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(apiPost).mockReset().mockResolvedValue({});
  vi.mocked(apiPut).mockReset().mockResolvedValue({});
  vi.mocked(apiDelete).mockReset().mockResolvedValue({});
});

it("creates a native mailbox profile without routing its secret through public row metadata", async () => {
  const user = userEvent.setup();
  const family = renderCredentialFamily(familyTemplate, { targets: [target] });
  act(() => family.openCreate());
  const dialog = within(screen.getByRole("dialog"));
  await user.type(dialog.getByRole("textbox", { name: "Mailbox address" }), "new@example.test");
  await user.type(dialog.getByRole("textbox", { name: "IMAP username" }), "new@example.test");
  await user.type(dialog.getByLabelText("IMAP password or app password"), "fixture-only-password");
  await user.click(dialog.getByRole("button", { name: "Create mail credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles", {
    kind: "password",
    label: "mailbox",
    risk_label: "mailbox access",
    public: {
      mailbox_address: "new@example.test",
      display_name: "",
      reply_to: "",
      imap_enabled: true,
      smtp_auth_mode: "disabled",
      allowed_read_folders: ["INBOX"],
      allowed_mutation_source_folders: ["INBOX"],
      allowed_mutation_destination_folders: [],
      sent_folder: "",
      archive_folder: "",
      trash_folder: "",
    },
    secret: { imap_username: "new@example.test", imap_password: "fixture-only-password" },
  });
  expect(screen.queryByText("fixture-only-password")).not.toBeInTheDocument();
});

it("composes dependent native updates when disabling IMAP and keeps stored login secrets on edit", async () => {
  const user = userEvent.setup();
  renderCredentialFamily(familyTemplate, { targets: [target] });
  await user.click(screen.getByRole("button", { name: "Edit credential" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByRole("textbox", { name: "Readable folders" })).toHaveValue("INBOX\nSent");
  await user.click(dialog.getByRole("checkbox", { name: "Enable IMAP mailbox access" }));
  expect(dialog.getByRole("checkbox", { name: "Enable IMAP mailbox access" })).not.toBeChecked();
  expect(dialog.getByRole("combobox", { name: "SMTP authentication" })).toHaveValue("disabled");
  expect(dialog.getByRole("button", { name: "Save mail credential" })).toBeDisabled();
  await user.click(dialog.getByRole("checkbox", { name: "Enable IMAP mailbox access" }));
  await user.click(dialog.getByRole("button", { name: "Save mail credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledOnce();
  expect(apiPut).toHaveBeenCalledWith(
    "/api/connector-targets/3/profiles/11",
    expect.objectContaining({
      public: expect.objectContaining({ imap_enabled: true, smtp_auth_mode: "disabled", allowed_read_folders: ["INBOX", "Sent"] }),
    }),
  );
  expect(vi.mocked(apiPut).mock.calls[0][1]).not.toHaveProperty("secret");
});

it("deletes only the local mailbox profile, never external messages", async () => {
  const user = userEvent.setup();
  const other = inventoryTargetFixture({
    ...target,
    id: 44,
    profiles: [
      inventoryProfileFixture({ connector_kind: "mail", target_id: 44, id: 22, label: "First mailbox" }),
      inventoryProfileFixture({ connector_kind: "mail", target_id: 44, id: 77, runtime_id: 91, label: "Selected mailbox" }),
    ],
  });
  renderCredentialFamily(familyTemplate, { targets: [target, other] });
  const row = screen.getByText("Selected mailbox").closest("tr");
  if (!row) throw new Error("Selected native mailbox is missing");
  await user.click(within(row).getByRole("button", { name: "Delete credential" }));
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiDelete).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/44/profiles/77");
  expect(apiPost).not.toHaveBeenCalled();
});

it("sends only the changed login secret when rotating a mailbox password", async () => {
  const user = userEvent.setup();
  renderCredentialFamily(familyTemplate, { targets: [target] });
  await user.click(screen.getByRole("button", { name: "Edit credential" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByRole("textbox", { name: "IMAP username" })).toHaveValue("");
  await user.type(dialog.getByLabelText("IMAP password or app password"), "fixture-new-password");
  await user.click(dialog.getByRole("button", { name: "Save mail credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith(
    "/api/connector-targets/3/profiles/11",
    expect.objectContaining({ secret: { imap_password: "fixture-new-password" } }),
  );
});

it("validates native mail fields while retaining inventory and profile identities", () => {
  const original = inventoryTargetFixture({
    ...target,
    profiles: [
      inventoryProfileFixture({
        connector_kind: "mail",
        runtime_id: 91,
        public: { mailbox_address: "support@example.test", allowed_read_folders: ["INBOX"], extension: "preserved" },
      }),
    ],
  });
  expect(mailCredentialTargets([original, inventoryTargetFixture({ config: { imap_port: [] } })])).toMatchObject([
    {
      id: 3,
      project_id: 7,
      profiles: [{ id: 11, runtime_id: 91, public: { mailbox_address: "support@example.test", extension: "preserved" } }],
    },
  ]);
  expect(() => mailCredentialTargets([inventoryTargetFixture({ ...target, config: { imap_port: [] } })])).toThrow("port");
  expect(() =>
    mailCredentialTargets([
      inventoryTargetFixture({ ...target, profiles: [inventoryProfileFixture({ public: { imap_enabled: "true" } })] }),
    ]),
  ).toThrow("imap_enabled");
  expect(mailCredentialTargets([inventoryTargetFixture({ ...target, config: undefined, profiles: undefined })])[0].profiles).toEqual([]);
});
