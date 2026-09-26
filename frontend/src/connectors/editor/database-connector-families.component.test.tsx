import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../test/connector-inventory-fixtures";
import { clickHouseConnectorFamily } from "../templates/clickhouse/connector-family";
import { postgresConnectorFamily } from "../templates/postgres/connector-family";
import { captureConnectorFamily } from "./capture-connector-family";
import type { ConnectorFamilyCommands, ConnectorFamilyProps } from "./connector-family-types";

vi.mock("../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn(), apiGet: vi.fn() }));
const refresh = vi.fn(async () => {});
let commands: ConnectorFamilyCommands | null = null;
const cases = [
  {
    kind: "postgres",
    name: "main-db",
    database: "postgres",
    tlsField: "ssl_mode",
    portLabel: "Port",
    risk: "read-only",
    family: postgresConnectorFamily.create(captureConnectorFamily),
  },
  {
    kind: "clickhouse",
    name: "analytics-db",
    database: "default",
    tlsField: "tls_mode",
    portLabel: "Native port",
    risk: "read-only analytics",
    family: clickHouseConnectorFamily.create(captureConnectorFamily),
  },
];
function renderNative(entry: (typeof cases)[number]) {
  const profile = inventoryProfileFixture({
    connector_kind: entry.kind,
    kind: "username_password",
    label: "readonly",
    public: { username: "reader" },
  });
  const target = inventoryTargetFixture({
    connector_kind: entry.kind,
    name: "My database",
    config: {
      host: "host.example",
      port: 8403,
      database: "my_db",
      [entry.tlsField]: "disable",
    },
    profiles: [profile],
  });
  const props: Omit<ConnectorFamilyProps, "children"> = {
    targets: [target],
    credentials: [],
    projects: [{ id: 7, name: "My Project" }],
    firstCredentialID: "",
    defaultProjectID: 7,
    connectorOptions: [{ kind: entry.kind, label: "Database" }],
    busy: false,
    register: (_kind, value) => {
      commands = value;
    },
    onOpen: vi.fn(),
    onSelectKind: vi.fn(),
    onStateChange: vi.fn(),
    onTestsChange: vi.fn(),
    refresh,
  };
  const Provider = entry.family.Provider;
  render(
    <Provider {...props}>
      <div>Inventory</div>
    </Provider>,
  );
  return { target, profile };
}
beforeEach(() => {
  vi.clearAllMocks();
  commands = null;
  vi.mocked(apiPost)
    .mockReset()
    .mockResolvedValue({ id: 3, profiles: [{ id: 11 }] });
  vi.mocked(apiPut)
    .mockReset()
    .mockResolvedValue({ id: 3, profiles: [{ id: 11 }] });
  vi.mocked(apiDelete).mockReset().mockResolvedValue({});
});

it.each(cases)("creates $kind through its owned form and serializer", async (entry) => {
  const user = userEvent.setup();
  renderNative(entry);
  act(() => commands?.openCreate());
  await user.type(screen.getByLabelText("Username"), "reader");
  await user.type(screen.getByLabelText("Password"), "test-password");
  await user.clear(screen.getByLabelText(entry.portLabel));
  await user.type(screen.getByLabelText(entry.portLabel), "8403");
  await user.click(screen.getByRole("button", { name: "Create connector" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/with-profile", {
    target: {
      connector_kind: entry.kind,
      name: entry.name,
      project_id: 7,
      config: {
        connection_mode: "direct",
        host: "127.0.0.1",
        port: 8403,
        database: entry.database,
        [entry.tlsField]: "auto",
        transport_target_ref: "",
      },
    },
    profile: {
      kind: "username_password",
      label: "readonly",
      public: { username: "reader" },
      secret: { password: "test-password" },
      risk_label: entry.risk,
    },
  });
  expect(refresh).toHaveBeenCalledOnce();
});

it.each(cases)("edits $kind without replacing its encrypted password", async (entry) => {
  const user = userEvent.setup();
  const { target, profile } = renderNative(entry);
  act(() => commands?.openEdit(target, profile));
  expect(screen.getByLabelText("Username")).toHaveValue("reader");
  await user.clear(screen.getByLabelText("Connector name"));
  await user.type(screen.getByLabelText("Connector name"), "Updated database");
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}/with-profile/${profile.id}`, {
    target: {
      name: "Updated database",
      project_id: 7,
      config: {
        connection_mode: "direct",
        host: "host.example",
        port: 8403,
        database: "my_db",
        [entry.tlsField]: "disable",
        transport_target_ref: "",
      },
    },
    profile: { kind: "username_password", label: "readonly", public: { username: "reader" }, risk_label: entry.risk },
  });
});

it.each(cases)("tests and deletes $kind using its decoded native identity", async (entry) => {
  const user = userEvent.setup();
  const { target, profile } = renderNative(entry);
  vi.mocked(apiPost).mockResolvedValueOnce({ ok: true });
  await act(async () => {
    expect(await commands?.test(target, profile)).toBe(true);
  });
  expect(apiPost).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}/profiles/${profile.id}/test`, {});
  expect(entry.family.tableTemplate.model.targetEndpoint?.({ target, profile })).toBe("host.example:8403/my_db · direct");
  act(() => commands?.requestDelete(target));
  await user.click(screen.getByRole("button", { name: "Delete connector" }));
  await waitFor(() => expect(apiDelete).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}`));
  expect(refresh).toHaveBeenCalledOnce();
});
