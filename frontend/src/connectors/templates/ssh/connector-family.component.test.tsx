import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { apiPost } from "../../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { captureConnectorFamily } from "../../editor/capture-connector-family";
import type { ConnectorFamilyCommands, ConnectorFamilyProps } from "../../editor/connector-family-types";
import { sshConnectorFamily } from "./connector-family";

vi.mock("../../../lib/api", () => ({
  apiPost: vi.fn(),
  apiPut: vi.fn(),
  apiDelete: vi.fn(),
  currentWorkspaceBinding: () => "family-workspace",
}));
const profile = inventoryProfileFixture({ public: { username: "operator", ssh_key_id: 9 } });
const target = inventoryTargetFixture({ profiles: [profile], config: { host: "host.example", port: 22 } });
const family = sshConnectorFamily.create(captureConnectorFamily);
const refresh = vi.fn(async () => {});
const onTestsChange = vi.fn();
let commands: ConnectorFamilyCommands | null = null;
const props: Omit<ConnectorFamilyProps, "children"> = {
  targets: [target],
  credentials: [
    { id: 90, name: "Other credential", connector_kind: "redis" },
    { id: 9, name: "Operator key", key_type: "ed25519", connector_kind: "ssh" },
  ],
  projects: [{ id: 7, name: "My Project" }],
  firstCredentialID: "90",
  defaultProjectID: 7,
  connectorOptions: [{ kind: "ssh", label: "SSH" }],
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
  const RowActions = family.tableTemplate.RowActions;
  return (
    <MemoryRouter>
      <Provider {...props}>{RowActions ? <RowActions target={target} profile={profile} onUnderConstruction={vi.fn()} /> : null}</Provider>
    </MemoryRouter>
  );
}
beforeEach(() => {
  vi.clearAllMocks();
  commands = null;
  vi.mocked(apiPost)
    .mockReset()
    .mockResolvedValue({ id: 3, profiles: [{ id: 11 }] });
});

it("selects only native key material and saves without consulting unrelated credentials", async () => {
  const user = userEvent.setup();
  render(<Host />);
  act(() => commands?.openCreate());
  expect(screen.getByRole("combobox", { name: "Credential profile key" })).toHaveValue("9");
  expect(screen.queryByRole("option", { name: /Other credential/ })).not.toBeInTheDocument();
  await user.type(screen.getByLabelText("Connector name"), "My host");
  await user.type(screen.getByLabelText("Host"), "host.example");
  await user.click(screen.getByRole("checkbox", { name: "I will install the key later" }));
  await user.click(screen.getByRole("button", { name: "Create connector" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/with-profile", {
    target: {
      connector_kind: "ssh",
      name: "My host",
      project_id: 7,
      config: {
        host: "host.example",
        port: 22,
        description: "",
        startup_input_after_connect: "",
        force_shell_command: "",
      },
    },
    profile: { kind: "private_key", label: "root", public: { username: "root", ssh_key_id: 9 } },
  });
  expect(refresh).toHaveBeenCalledOnce();
});

it("retains native fingerprint approval and resumes the original profile test", async () => {
  const user = userEvent.setup();
  render(<Host />);
  const hostKey = {
    host: "host.example",
    hostname: "host.example:22",
    port: 22,
    public_key: "ssh-ed25519 test-key",
    fingerprint_sha256: "SHA256:test",
    key_type: "ssh-ed25519",
    changed: false,
  };
  vi.mocked(apiPost).mockRejectedValueOnce({ status: 409, data: { code: "unknown_ssh_host_key", host_key: hostKey } });
  await act(async () => {
    expect(await commands?.test(target, profile)).toBe(false);
  });
  expect(await screen.findByRole("dialog", { name: "Approve SSH host fingerprint" })).toBeInTheDocument();
  vi.mocked(apiPost).mockResolvedValueOnce({}).mockResolvedValueOnce({ ok: true });
  await user.click(screen.getByRole("button", { name: "Approve fingerprint" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenNthCalledWith(
    2,
    "/api/connectors/ssh/host-keys/approve",
    {
      host: hostKey.host,
      port: hostKey.port,
      public_key: hostKey.public_key,
      replace: false,
    },
    { signal: expect.any(AbortSignal) },
  );
  expect(apiPost).toHaveBeenNthCalledWith(3, `/api/connector-targets/${target.id}/profiles/${profile.id}/test`, {});
  expect(onTestsChange).toHaveBeenLastCalledWith(
    "ssh",
    expect.objectContaining({ [`ssh:${target.id}:${profile.id}`]: expect.objectContaining({ state: "ok" }) }),
  );
  expect(refresh).not.toHaveBeenCalled();
});

it("unmounts native operation requests when the family closes", async () => {
  const user = userEvent.setup();
  render(<Host />);
  let finish!: (_result: object) => void;
  vi.mocked(apiPost).mockReturnValueOnce(
    new Promise((resolve) => {
      finish = resolve;
    }),
  );
  await user.click(screen.getByRole("button", { name: `Check Docker for ${target.name}` }));
  await waitFor(() =>
    expect(apiPost).toHaveBeenCalledWith(
      `/api/connector-targets/${target.id}/operations/docker-check`,
      { profile_id: profile.id },
      { signal: expect.any(AbortSignal) },
    ),
  );
  const signal = vi.mocked(apiPost).mock.calls[0]?.[2]?.signal;
  act(() => commands?.close());
  expect(signal?.aborted).toBe(true);
  await act(async () => {
    finish({ ok: true, containers: [] });
  });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
