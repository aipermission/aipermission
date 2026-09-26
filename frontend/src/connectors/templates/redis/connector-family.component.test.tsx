import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { captureConnectorFamily } from "../../editor/capture-connector-family";
import type { ConnectorFamilyCommands, ConnectorFamilyProps } from "../../editor/connector-family-types";
import { redisConnectorFamily } from "./connector-family";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn() }));
const profile = inventoryProfileFixture({ connector_kind: "redis", kind: "username_password", public: { username: "reader" } });
const target = inventoryTargetFixture({
  connector_kind: "redis",
  name: "Cache",
  config: { host: "localhost", port: 6379, server_family: "valkey" },
  profiles: [profile],
});
const family = redisConnectorFamily.create(captureConnectorFamily);
const refresh = vi.fn(async () => {});
const onTestsChange = vi.fn();
let commands: ConnectorFamilyCommands | null = null;
const props: Omit<ConnectorFamilyProps, "children"> = {
  targets: [target],
  credentials: [],
  projects: [{ id: 7, name: "My Project" }],
  firstCredentialID: "",
  defaultProjectID: 7,
  connectorOptions: [{ kind: "redis", label: "Redis / Valkey" }],
  busy: false,
  register: (_kind, value) => {
    commands = value;
  },
  onOpen: vi.fn(),
  onSelectKind: vi.fn(),
  onStateChange: vi.fn(),
  onTestsChange,
  refresh,
};
function Host() {
  const Provider = family.Provider;
  return (
    <Provider {...props}>
      <div>Inventory</div>
    </Provider>
  );
}
beforeEach(() => {
  vi.clearAllMocks();
  commands = null;
  vi.mocked(apiPost)
    .mockReset()
    .mockResolvedValue({ id: 30, profiles: [{ id: 40 }] });
  vi.mocked(apiPut)
    .mockReset()
    .mockResolvedValue({ id: target.id, profiles: [{ id: profile.id }] });
  vi.mocked(apiDelete).mockReset().mockResolvedValue({});
});

it("creates native Valkey connection fields through the generic drawer", async () => {
  const user = userEvent.setup();
  render(<Host />);
  act(() => commands?.openCreate());
  await user.selectOptions(screen.getByRole("combobox", { name: "Server product" }), "valkey");
  await user.type(screen.getByLabelText("Password"), "test-password");
  await user.click(screen.getByRole("button", { name: "Create connector" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/with-profile", {
    target: {
      connector_kind: "redis",
      name: "redis-cache",
      project_id: 7,
      config: {
        server_family: "valkey",
        connection_mode: "direct",
        host: "127.0.0.1",
        port: 6379,
        database: 0,
        tls_mode: "auto",
        transport_target_ref: "",
      },
    },
    profile: {
      kind: "username_password",
      label: "default",
      public: { username: "" },
      secret: { password: "test-password" },
      risk_label: "cache access",
    },
  });
  expect(refresh).toHaveBeenCalledOnce();
});

it("edits the decoded profile without replacing an untouched password", async () => {
  const user = userEvent.setup();
  render(<Host />);
  act(() => commands?.openEdit(target, profile));
  expect(screen.getByRole("combobox", { name: "Server product" })).toHaveValue("valkey");
  expect(screen.getByLabelText("Username")).toHaveValue("reader");
  await user.clear(screen.getByLabelText("Connector name"));
  await user.type(screen.getByLabelText("Connector name"), "Updated cache");
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}/with-profile/${profile.id}`, {
    target: {
      name: "Updated cache",
      project_id: 7,
      config: {
        server_family: "valkey",
        connection_mode: "direct",
        host: "localhost",
        port: 6379,
        database: 0,
        tls_mode: "disable",
        transport_target_ref: "",
      },
    },
    profile: { kind: "username_password", label: profile.label, public: { username: "reader" }, risk_label: "cache access" },
  });
});

it("tests and deletes the native inventory identity through common commands", async () => {
  const user = userEvent.setup();
  render(<Host />);
  vi.mocked(apiPost).mockResolvedValueOnce({ ok: true, message: "Reachable" });
  await act(async () => {
    expect(await commands?.test(target, profile)).toBe(true);
  });
  expect(apiPost).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}/profiles/${profile.id}/test`, {});
  expect(onTestsChange).toHaveBeenLastCalledWith(
    "redis",
    expect.objectContaining({ [`redis:${target.id}:${profile.id}`]: expect.objectContaining({ state: "ok" }) }),
  );
  act(() => commands?.requestDelete(target));
  await user.click(screen.getByRole("button", { name: "Delete connector" }));
  await waitFor(() => expect(apiDelete).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}`));
  expect(refresh).toHaveBeenCalledOnce();
});
