import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { renderCredentialFamily } from "../../../test/render-credential-family";
import { captureCredentialFamily } from "../../editor/capture-credential-family";
import { postgresCredentialFamily, postgresCredentialTargets } from "./credential-family";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn() }));
const familyTemplate = postgresCredentialFamily.create(captureCredentialFamily);
const target = inventoryTargetFixture({
  connector_kind: "postgres",
  name: "Test database",
  config: { host: "localhost", port: 5432, database: "test" },
  profiles: [
    inventoryProfileFixture({ connector_kind: "postgres", kind: "username_password", label: "Reader", public: { username: "reader" } }),
  ],
});
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(apiPost).mockReset().mockResolvedValue({});
  vi.mocked(apiPut).mockReset().mockResolvedValue({});
  vi.mocked(apiDelete).mockReset().mockResolvedValue({});
});

it("creates an ordinary Postgres credential through its native form", async () => {
  const user = userEvent.setup();
  const family = renderCredentialFamily(familyTemplate, {
    targets: [target, inventoryTargetFixture({ ...target, id: 44, name: "Other database", profiles: [] })],
  });
  act(() => family.openCreate());
  const dialog = within(screen.getByRole("dialog"));
  await user.selectOptions(dialog.getByRole("combobox", { name: "Connector target" }), "44");
  await user.type(dialog.getByRole("textbox", { name: "Username" }), "new_reader");
  await user.type(dialog.getByLabelText("Password"), "fixture-only-password");
  await user.click(dialog.getByRole("button", { name: "Create Postgres credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/44/profiles", {
    kind: "username_password",
    label: "readonly",
    public: { username: "new_reader" },
    risk_label: "read-only",
    secret: { password: "fixture-only-password" },
  });
  expect(family.refresh).toHaveBeenCalledOnce();
});

it("keeps managed role identity immutable while allowing a local label edit", async () => {
  const user = userEvent.setup();
  const managedTarget = inventoryTargetFixture({
    ...target,
    profiles: [
      inventoryProfileFixture({
        connector_kind: "postgres",
        kind: "username_password",
        label: "Managed reader",
        public: {
          username: "managed_reader",
          managed_by_aipermission: true,
          managed_role_name: "managed_reader",
          managed_admin_profile_id: 22,
        },
      }),
    ],
  });
  renderCredentialFamily(familyTemplate, { targets: [managedTarget] });
  await user.click(screen.getByRole("button", { name: "Edit credential" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByRole("textbox", { name: "Username" })).toBeDisabled();
  expect(dialog.getByRole("textbox", { name: "Username" })).toHaveValue("managed_reader");
  expect(dialog.getByLabelText("New password")).toBeDisabled();
  await user.clear(dialog.getByRole("textbox", { name: "Profile label" }));
  await user.type(dialog.getByRole("textbox", { name: "Profile label" }), "Renamed reader");
  await user.click(dialog.getByRole("button", { name: "Save Postgres credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles/11", {
    kind: "username_password",
    label: "Renamed reader",
    public: { username: "managed_reader" },
    risk_label: "read-only",
  });
});

it("preserves managed deletion metadata and sends only the selected native profile deletion", async () => {
  const user = userEvent.setup();
  const managedTarget = inventoryTargetFixture({
    ...target,
    profiles: [
      inventoryProfileFixture({
        connector_kind: "postgres",
        label: "Managed reader",
        public: {
          username: "managed_reader",
          managed_by_aipermission: true,
          managed_role_name: "managed_reader",
          managed_admin_profile_id: 22,
        },
      }),
      inventoryProfileFixture({ connector_kind: "postgres", id: 22, label: "Admin", public: { username: "test_admin" } }),
    ],
  });
  renderCredentialFamily(familyTemplate, { targets: [managedTarget] });
  const row = screen.getByText("Managed reader").closest("tr");
  if (!row) throw new Error("Managed row is missing");
  await user.click(within(row).getByRole("button", { name: "Delete credential" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByText(/reassigned to test_admin/)).toBeVisible();
  expect(dialog.getByText("managed_reader", { selector: "span" })).toBeVisible();
  await user.click(dialog.getByRole("button", { name: "Delete role and credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiDelete).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles/11");
});

it("validates owned managed metadata and retains independent runtime identity", () => {
  const [decoded] = postgresCredentialTargets([
    inventoryTargetFixture({
      ...target,
      profiles: [
        inventoryProfileFixture({
          connector_kind: "postgres",
          runtime_id: 91,
          public: { username: "reader", managed_by_aipermission: false },
        }),
      ],
    }),
    inventoryTargetFixture(),
  ]);
  expect(decoded).toMatchObject({
    id: 3,
    project_id: 7,
    profiles: [{ id: 11, runtime_id: 91, public: { username: "reader", managed_by_aipermission: false } }],
  });
  for (const publicMetadata of [{ managed_by_aipermission: "true" }, { managed_admin_profile_id: "22" }, { managed_role_name: [] }]) {
    expect(() =>
      postgresCredentialTargets([inventoryTargetFixture({ ...target, profiles: [inventoryProfileFixture({ public: publicMetadata })] })]),
    ).toThrow("Postgres");
  }
});
