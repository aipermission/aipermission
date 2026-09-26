import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useCallback, useState } from "react";
import { flushSync } from "react-dom";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import type { CredentialFamilyCommands, CredentialFamilyProps } from "../../editor/credential-family-types";
import { sshCredentialFamily, sshCredentialTargets } from "./credential-family";
import { captureCredentialFamily } from "../../editor/capture-credential-family";
import { isCredentialFamilyRegistration } from "../_shared/credential-family-registration";
import { renderCredentialFamily } from "../../../test/render-credential-family";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn() }));
const familyTemplate = sshCredentialFamily.create(captureCredentialFamily);
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(apiPost).mockReset().mockResolvedValue({});
  vi.mocked(apiPut).mockReset().mockResolvedValue({});
  vi.mocked(apiDelete).mockReset().mockResolvedValue({});
});

it("accepts only factory-created frozen registrations at the dynamic module boundary", () => {
  expect(isCredentialFamilyRegistration(sshCredentialFamily)).toBe(true);
  expect(Object.isFrozen(sshCredentialFamily)).toBe(true);
  expect(isCredentialFamilyRegistration({ ...sshCredentialFamily })).toBe(false);
  expect(isCredentialFamilyRegistration({ kind: "ssh", create: () => familyTemplate })).toBe(false);
  expect(isCredentialFamilyRegistration(null)).toBe(false);
  expect(isCredentialFamilyRegistration("ssh")).toBe(false);
});

function renderFamily(overrides: Partial<CredentialFamilyProps> = {}) {
  return renderCredentialFamily(familyTemplate, {
    credentials: [{ id: 5, name: "Test key", key_type: "ed25519", connector_kind: "ssh", install_command: "Install test public key" }],
    ...overrides,
  });
}

it("captures native generation state, forwards the exact operation, and exposes only a command to the host", async () => {
  vi.mocked(apiPost).mockResolvedValue({});
  const user = userEvent.setup();
  const family = renderFamily();
  act(() => family.openCreate());
  const dialog = screen.getByRole("dialog");
  await user.click(within(dialog).getByRole("button", { name: "rsa" }));
  await user.click(within(dialog).getByRole("button", { name: "Generate rsa credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledWith("/api/connectors/ssh/credentials", { name: "main", key_type: "rsa" });
  expect(family.refresh).toHaveBeenCalledOnce();
  expect(family.onOpen).toHaveBeenCalledWith("ssh");
  expect(family.onStateChange).toHaveBeenLastCalledWith("ssh", { state: "idle", error: null, message: "SSH credential created." });
  family.unmount();
  expect(family.register).toHaveBeenLastCalledWith("ssh", null);
});

it("ignores commands retained by the host after the family unmounts", () => {
  let retained: CredentialFamilyCommands | null = null;
  const family = renderFamily({
    register: (_kind, commands) => {
      if (commands) retained = commands;
    },
  });
  if (!retained) throw new Error("Missing native credential commands");
  const commands: CredentialFamilyCommands = retained;
  family.unmount();
  act(() => {
    commands.openCreate();
    commands.close();
  });
  expect(family.onOpen).not.toHaveBeenCalled();
  expect(apiPost).not.toHaveBeenCalled();
});

it("registers stable commands even when the host stores them in React state", async () => {
  const user = userEvent.setup();
  const registrations = vi.fn();
  function Host() {
    const [commands, setCommands] = useState<CredentialFamilyCommands | null>(null);
    const register = useCallback((_kind: string, next: CredentialFamilyCommands | null) => {
      registrations(next);
      setCommands(next);
    }, []);
    return (
      <>
        <button onClick={() => commands?.openCreate()}>Create native credential</button>
        <table>
          <tbody>
            <familyTemplate.Rows
              targets={[]}
              credentials={[]}
              busy={false}
              register={register}
              onOpen={() => {}}
              onStateChange={() => {}}
              refresh={async () => {}}
            />
          </tbody>
        </table>
      </>
    );
  }
  render(<Host />);
  expect(registrations).toHaveBeenCalledOnce();
  await user.click(screen.getByRole("button", { name: "Create native credential" }));
  await user.type(within(screen.getByRole("dialog")).getByRole("textbox", { name: "Name" }), "suffix");
  expect(registrations).toHaveBeenCalledOnce();
});

it("rejects an imperative create while another family is busy", () => {
  const family = renderFamily({ busy: true });
  act(() => family.openCreate());
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(family.onOpen).not.toHaveBeenCalled();
  expect(apiPost).not.toHaveBeenCalled();
});

it("rechecks availability if the host changes state synchronously during activation", () => {
  const onOpen = vi.fn(() =>
    flushSync(() => {
      family.rerender(
        <table>
          <tbody>
            <familyTemplate.Rows {...family.props} busy />
          </tbody>
        </table>,
      );
    }),
  );
  const family = renderFamily({ onOpen });
  act(() => family.openCreate());
  expect(onOpen).toHaveBeenCalledOnce();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(apiPost).not.toHaveBeenCalled();
});

it("guards a form submission when another family becomes busy after the drawer opens", () => {
  const family = renderFamily();
  act(() => family.openCreate());
  family.rerender(
    <table>
      <tbody>
        <familyTemplate.Rows {...family.props} busy />
      </tbody>
    </table>,
  );
  const dialog = screen.getByRole("dialog");
  const form = within(dialog).getByRole("button", { name: "Generate ed25519 credential" }).closest("form");
  if (!form) throw new Error("Missing native credential form");
  expect(within(dialog).getByRole("textbox", { name: "Name" })).toBeDisabled();
  fireEvent.submit(form);
  expect(apiPost).not.toHaveBeenCalled();
});

it("guards confirmation when another family becomes busy after deletion is requested", async () => {
  const user = userEvent.setup();
  const family = renderFamily();
  await user.click(screen.getByRole("button", { name: "Delete credential" }));
  family.rerender(
    <table>
      <tbody>
        <familyTemplate.Rows {...family.props} busy />
      </tbody>
    </table>,
  );
  const confirm = within(screen.getByRole("dialog")).getByRole("button", { name: "Delete credential" });
  expect(confirm).toBeDisabled();
  await user.click(confirm);
  expect(apiDelete).not.toHaveBeenCalled();
});

it("preserves the connector-owned installation action and native edit row", async () => {
  vi.mocked(apiPut).mockResolvedValue({});
  const user = userEvent.setup();
  renderFamily();
  expect(screen.getByRole("button", { name: "Copy install command" })).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Edit credential" }));
  const dialog = screen.getByRole("dialog");
  expect(within(dialog).getByRole("textbox", { name: "Name" })).toHaveValue("Test key");
  await user.clear(within(dialog).getByRole("textbox", { name: "Name" }));
  await user.type(within(dialog).getByRole("textbox", { name: "Name" }), "Renamed key");
  await user.click(within(dialog).getByRole("button", { name: "Save SSH credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledWith("/api/connectors/ssh/credentials/5", { name: "Renamed key" });
});

it("keeps dialogs out of table DOM and clears native import secrets on close", async () => {
  const user = userEvent.setup();
  const family = renderFamily();
  act(() => family.openCreate());
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Import" }));
  await user.type(within(screen.getByRole("dialog")).getByRole("textbox", { name: "Private key" }), "fixture-only-key-data");
  expect(family.container.querySelector("tbody")?.children).toHaveLength(1);
  expect(family.container.querySelector('[role="dialog"]')).toBeNull();
  act(() => family.close());
  act(() => family.openCreate());
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Import" }));
  expect(within(screen.getByRole("dialog")).getByRole("textbox", { name: "Private key" })).toHaveValue("");
});

it("does not let a retired native save close a replacement import draft", async () => {
  let finish!: (_value: object) => void;
  vi.mocked(apiPost).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const user = userEvent.setup();
  const family = renderFamily();
  act(() => family.openCreate());
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Generate ed25519 credential" }));
  expect(within(screen.getByRole("dialog")).getByRole("textbox", { name: "Name" })).toBeDisabled();
  act(() => family.close());
  act(() => family.openCreate());
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Import" }));
  await user.type(within(screen.getByRole("dialog")).getByRole("textbox", { name: "Private key" }), "replacement-key-data");
  await act(async () => finish({}));
  expect(within(screen.getByRole("dialog")).getByRole("textbox", { name: "Private key" })).toHaveValue("replacement-key-data");
  expect(family.refresh).not.toHaveBeenCalled();
});

it("retains native key links and disables deletion of linked credentials", () => {
  renderFamily({ targets: [inventoryTargetFixture({ profiles: [inventoryProfileFixture({ public: { ssh_key_id: 5 } })] })] });
  expect(screen.getByRole("button", { name: "Remove connector links first" })).toBeDisabled();
  expect(screen.getByText("Test host")).toBeVisible();
});

it("deletes only the selected native key after confirmation", async () => {
  vi.mocked(apiDelete).mockResolvedValue({});
  const user = userEvent.setup();
  const family = renderFamily();
  await user.click(screen.getByRole("button", { name: "Delete credential" }));
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiDelete).toHaveBeenCalledWith("/api/connectors/ssh/credentials/5");
  expect(family.refresh).toHaveBeenCalledOnce();
});

it("decodes owned target fields without conflating profile and runtime identities or reading another kind", () => {
  const target = inventoryTargetFixture({
    config: { host: "localhost", port: "22", force_shell_command: "bash" },
    profiles: [inventoryProfileFixture({ runtime_id: 91, public: { username: "operator", ssh_key_id: "5" } })],
  });
  const decoded = sshCredentialTargets([target, inventoryTargetFixture({ connector_kind: "redis", config: { host: [] } })]);
  expect(decoded).toHaveLength(1);
  expect(decoded[0]).toMatchObject({
    id: 3,
    project_id: 7,
    config: target.config,
    profiles: [{ id: 11, runtime_id: 91, public: { username: "operator", ssh_key_id: "5" } }],
  });
  expect(decoded[0]).not.toBe(target);
  expect(() => sshCredentialTargets([inventoryTargetFixture({ config: { host: [] } })])).toThrow("host");
  expect(() =>
    sshCredentialTargets([inventoryTargetFixture({ profiles: [inventoryProfileFixture({ public: { ssh_key_id: {} } })] })]),
  ).toThrow("ssh_key_id");
});
