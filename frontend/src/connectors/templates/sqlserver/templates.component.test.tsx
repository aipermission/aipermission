import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { renderCredentialFamily } from "../../../test/render-credential-family";
import { captureCredentialFamily } from "../../editor/capture-credential-family";
import { captureConnectorFamily } from "../../editor/capture-connector-family";
import type { ConnectorFamilyCommands } from "../../editor/connector-family-types";
import { sqlServerCredentialFamily, sqlServerTargets } from "./credential-family";
import { sqlServerConnectorFamily } from "./connector-family";
import { sqlServerConsoleConfig } from "./console";
import * as model from "./model";

vi.mock("../../../lib/api", () => ({
  apiPost: vi.fn(),
  apiPut: vi.fn(),
  apiDelete: vi.fn(),
  apiGet: vi.fn(async () => []),
  currentWorkspaceBinding: () => "sql-test",
}));
const profile = inventoryProfileFixture({
  id: 17,
  connector_kind: "sqlserver",
  kind: "username_password",
  label: "Reader",
  public: { username: "reader" },
});
const target = inventoryTargetFixture({
  connector_kind: "sqlserver",
  name: "My database",
  config: { host: "db.test", port: 1433, database: "App", tls_mode: "verify_full" },
  profiles: [profile],
});
const credentialFamily = sqlServerCredentialFamily.create(captureCredentialFamily);
const connectorFamily = sqlServerConnectorFamily.create(captureConnectorFamily);

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(apiPost)
    .mockReset()
    .mockResolvedValue({ id: 3, profiles: [profile] });
  vi.mocked(apiPut)
    .mockReset()
    .mockResolvedValue({ id: 3, profiles: [profile] });
  vi.mocked(apiDelete).mockReset().mockResolvedValue({});
});

it("creates a direct SQL target and default profile through the generic atomic save", async () => {
  const user = userEvent.setup();
  let commands: ConnectorFamilyCommands | null = null;
  render(
    <connectorFamily.Provider
      targets={[target]}
      credentials={[]}
      projects={[{ id: 7, name: "My Project" }]}
      defaultProjectID={7}
      firstCredentialID=""
      connectorOptions={[{ kind: "sqlserver", label: "SQL Server" }]}
      busy={false}
      register={(_kind, value) => {
        commands = value;
      }}
      onOpen={vi.fn()}
      onSelectKind={vi.fn()}
      onStateChange={vi.fn()}
      onTestsChange={vi.fn()}
      refresh={vi.fn(async () => {})}
    >
      <div />
    </connectorFamily.Provider>,
  );
  act(() => commands?.openCreate());
  expect(screen.getByLabelText("TLS mode")).toHaveValue("verify_full");
  expect(screen.queryByLabelText("SSH connector profile")).not.toBeInTheDocument();
  await user.clear(screen.getByLabelText("Connector name"));
  await user.type(screen.getByLabelText("Connector name"), "My SQL database");
  await user.selectOptions(screen.getByLabelText("TLS mode"), "disable");
  expect(screen.getByText(/TLS is disabled/)).toBeVisible();
  await user.selectOptions(screen.getByLabelText("TLS mode"), "verify_full");
  await user.type(screen.getByLabelText("TLS server name"), "certificate.example.test");
  await user.type(screen.getByLabelText("Trusted CA certificate"), "fixture-public-certificate");
  await user.clear(screen.getByLabelText("Profile label"));
  await user.type(screen.getByLabelText("Profile label"), "readonly");
  await user.clear(screen.getByLabelText("Risk label"));
  await user.type(screen.getByLabelText("Risk label"), "read-only");
  await user.type(screen.getByLabelText("SQL Server host"), "sql.example.test");
  await user.type(screen.getByLabelText("Database", { exact: true }), "App");
  await user.type(screen.getByLabelText("Username"), "reader");
  await user.type(screen.getByLabelText("Password", { exact: true }), "fixture-only");
  await user.click(screen.getByRole("button", { name: "Create connector" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledWith("/api/connector-targets/with-profile", {
    target: {
      connector_kind: "sqlserver",
      name: "My SQL database",
      project_id: 7,
      config: {
        connection_mode: "direct",
        host: "sql.example.test",
        port: 1433,
        database: "App",
        tls_mode: "verify_full",
        tls_server_name: "certificate.example.test",
        ca_certificate: "fixture-public-certificate",
      },
    },
    profile: {
      kind: "username_password",
      label: "readonly",
      public: { username: "reader" },
      secret: { password: "fixture-only" },
      risk_label: "read-only",
    },
  });
  act(() => commands?.openEdit(target, profile));
  expect(screen.getByLabelText("New password")).not.toBeRequired();
  await user.clear(screen.getByLabelText("Username"));
  await user.type(screen.getByLabelText("Username"), "changed-reader");
  expect(screen.getByLabelText("New password")).toBeRequired();
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  expect(apiPut).not.toHaveBeenCalled();
  await user.type(screen.getByLabelText("New password"), "replacement-fixture");
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() =>
    expect(apiPut).toHaveBeenCalledWith(
      "/api/connector-targets/3/with-profile/17",
      expect.objectContaining({
        profile: expect.objectContaining({ public: { username: "changed-reader" }, secret: { password: "replacement-fixture" } }),
      }),
    ),
  );
});

it("creates credentials and edits the selected profile without exposing a password", async () => {
  const user = userEvent.setup();
  const family = renderCredentialFamily(credentialFamily, { targets: [target] });
  act(() => family.openCreate());
  let dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByLabelText("Password")).toBeRequired();
  await user.type(dialog.getByLabelText("Username"), "second");
  await user.type(dialog.getByLabelText("Password"), "fixture-only");
  await user.click(dialog.getByRole("button", { name: "Create SQL Server credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledWith(
    "/api/connector-targets/3/profiles",
    expect.objectContaining({ public: { username: "second" }, secret: { password: "fixture-only" } }),
  );
  await user.click(screen.getByRole("button", { name: "Edit credential" }));
  dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByLabelText("New password")).toHaveValue("");
  expect(dialog.getByLabelText("New password")).not.toBeRequired();
  await user.clear(dialog.getByLabelText("Username"));
  await user.type(dialog.getByLabelText("Username"), "changed-reader");
  expect(dialog.getByLabelText("New password")).toBeRequired();
  await user.click(dialog.getByRole("button", { name: "Save SQL Server credential" }));
  expect(apiPut).not.toHaveBeenCalled();
  await user.clear(dialog.getByLabelText("Username"));
  await user.type(dialog.getByLabelText("Username"), "reader");
  expect(dialog.getByLabelText("New password")).not.toBeRequired();
  await user.click(dialog.getByRole("button", { name: "Save SQL Server credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledWith("/api/connector-targets/3/profiles/17", expect.not.objectContaining({ secret: expect.anything() }));
});

it("uses generic local credential deletion, not remote user deletion", async () => {
  const user = userEvent.setup();
  renderCredentialFamily(credentialFamily, { targets: [target] });
  await user.click(screen.getByRole("button", { name: "Delete credential" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.queryByRole("button", { name: "Delete role and credential" })).not.toBeInTheDocument();
  await user.click(dialog.getByRole("button", { name: "Delete credential" }));
  await waitFor(() => expect(apiDelete).toHaveBeenCalledWith("/api/connector-targets/3/profiles/17"));
});

it("retains SQL Server field identity and rejects malformed connector-owned settings", () => {
  expect(sqlServerTargets([target, inventoryTargetFixture()])).toHaveLength(1);
  expect(() => sqlServerTargets([inventoryTargetFixture({ ...target, config: { tls_mode: [] } })])).toThrow("tls_mode");
  const form = model.formFromTarget({ target, profile });
  expect(form).toMatchObject({ profile_id: "17", database: "App", tls_mode: "verify_full", password: "" });
  expect(model.usesLiveConsole()).toBe(false);
  expect(sqlServerConsoleConfig.metadataAction).toBe("get_metadata");
  expect(sqlServerConsoleConfig.describeInput({ schema: "", table: "Orders" })).toEqual({ schema: "dbo", table: "Orders" });
  expect(sqlServerConsoleConfig.tableQuery({ schema: " tenant ", table: "a]b" }, 25)).toBe("SELECT TOP (25) *\nFROM [ tenant ].[a]]b];");
});

it("restores target defaults and omits unused TLS material in explicitly disabled mode", async () => {
  const emptyTarget = inventoryTargetFixture({ connector_kind: "sqlserver", config: {}, profiles: [] });
  const form = model.formFromTarget({ target: emptyTarget });
  expect(form).toMatchObject({ host: "", port: 1433, database: "", tls_mode: "verify_full", tls_server_name: "", ca_certificate: "" });
  expect(model.targetEndpoint({ target: emptyTarget })).toBe("host:1433/database · direct");
  await model.save({
    mode: "create",
    form: {
      ...form,
      host: "db.test",
      database: "App",
      username: "reader",
      password: "fixture-only",
      tls_mode: "disable",
      tls_server_name: "previous.example.test",
      ca_certificate: "previous-public-certificate",
    },
  });
  expect(apiPost).toHaveBeenCalledWith(
    "/api/connector-targets/with-profile",
    expect.objectContaining({
      target: expect.objectContaining({
        config: expect.objectContaining({ tls_mode: "disable", tls_server_name: "", ca_certificate: "" }),
      }),
    }),
  );
});
