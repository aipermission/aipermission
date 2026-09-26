import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiGet, apiPost, apiPut } from "../lib/api";
import { useGateway } from "../lib/gateway-context";
import { inventoryProfileFixture, inventoryTargetFixture } from "../test/connector-inventory-fixtures";
import { CredentialsPage } from "./credentials";

vi.mock("../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn() }));
vi.mock("../lib/gateway-context", () => ({ useGateway: vi.fn() }));

const redisTarget = inventoryTargetFixture({
  connector_kind: "redis",
  name: "Test cache",
  config: { host: "localhost", port: 6379, database: 2 },
  profiles: [
    inventoryProfileFixture({ connector_kind: "redis", kind: "username_password", label: "Reader", public: { username: "reader" } }),
  ],
});
const key = {
  id: 5,
  connector_kind: "ssh",
  resource_kind: "ssh_key",
  resource_ref: "ssh-key:5",
  name: "Test key",
  key_type: "ed25519",
  fingerprint: "Test fingerprint",
  install_command: "Install test public key",
};
const loadCredentials = vi.fn(async () => []);

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(apiGet).mockImplementation(async (path) => {
    if (path === "/api/connectors")
      return {
        items: [
          { kind: "redis", label: "Redis", version: "0.2" },
          { kind: "ssh", label: "SSH", version: "0.2" },
        ],
      };
    if (path === "/api/connector-targets/inventory") return { items: [redisTarget] };
    throw new Error(`Unexpected test request: ${path}`);
  });
  vi.mocked(apiPost).mockResolvedValue({});
  vi.mocked(apiPut).mockResolvedValue({});
  vi.mocked(apiDelete).mockResolvedValue({});
  vi.mocked(useGateway, { partial: true }).mockReturnValue({
    credentials: { state: "ready", data: [key], errors: [], error: null },
    loadCredentials,
  });
});

async function openCreate(kind: string) {
  const user = userEvent.setup();
  render(<CredentialsPage />);
  await screen.findByText("Reader");
  await user.click(screen.getByRole("button", { name: "Add credential" }));
  await user.click(screen.getByRole("menuitem", { name: new RegExp(kind) }));
  return { user, dialog: screen.getByRole("dialog") };
}

it("creates a native Redis profile and refreshes both resource owners without returning the secret to the table", async () => {
  const { user, dialog } = await openCreate("Redis");
  await user.clear(within(dialog).getByRole("textbox", { name: "Profile label" }));
  await user.type(within(dialog).getByRole("textbox", { name: "Profile label" }), "Writer");
  await user.type(within(dialog).getByRole("textbox", { name: "Username" }), "writer");
  await user.type(within(dialog).getByLabelText("Password"), "fixture-only-password");
  await user.click(within(dialog).getByRole("button", { name: "Create Redis credential" }));

  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles", {
    kind: "username_password",
    label: "Writer",
    public: { username: "writer" },
    secret: { password: "fixture-only-password" },
    risk_label: "cache access",
  });
  expect(loadCredentials).toHaveBeenCalledOnce();
  expect(vi.mocked(apiGet).mock.calls.filter(([path]) => path === "/api/connector-targets/inventory")).toHaveLength(2);
  expect(screen.getByText("Redis credential created.")).toBeVisible();
  expect(screen.queryByText("fixture-only-password")).not.toBeInTheDocument();
});

it("edits a native Redis profile without sending an empty replacement secret", async () => {
  const user = userEvent.setup();
  render(<CredentialsPage />);
  const row = (await screen.findByText("Reader")).closest("tr");
  if (!row) throw new Error("Missing Redis row");
  await user.click(within(row).getByRole("button", { name: "Edit credential" }));
  const dialog = screen.getByRole("dialog");
  expect(within(dialog).getByRole("textbox", { name: "Username" })).toHaveValue("reader");
  expect(within(dialog).getByLabelText("New password")).toHaveValue("");
  await user.clear(within(dialog).getByRole("textbox", { name: "Profile label" }));
  await user.type(within(dialog).getByRole("textbox", { name: "Profile label" }), "Renamed reader");
  await user.click(within(dialog).getByRole("button", { name: "Save Redis credential" }));

  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles/11", {
    kind: "username_password",
    label: "Renamed reader",
    public: { username: "reader" },
    risk_label: "",
  });
});

it("delegates SSH key generation to its native controller instead of the target-profile endpoint", async () => {
  const { user, dialog } = await openCreate("SSH");
  await user.clear(within(dialog).getByRole("textbox", { name: "Name" }));
  await user.type(within(dialog).getByRole("textbox", { name: "Name" }), "Generated test key");
  await user.click(within(dialog).getByRole("button", { name: "rsa" }));
  await user.click(within(dialog).getByRole("button", { name: "Generate rsa credential" }));

  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connectors/ssh/credentials", { name: "Generated test key", key_type: "rsa" });
  expect(loadCredentials).toHaveBeenCalledOnce();
});

it("keeps a failed native save editable and clears the secret after a successful retry", async () => {
  vi.mocked(apiPost).mockRejectedValueOnce(new Error("Temporary save failure")).mockResolvedValueOnce({});
  const { user, dialog } = await openCreate("Redis");
  await user.type(within(dialog).getByLabelText("Password"), "fixture-only-password");
  await user.click(within(dialog).getByRole("button", { name: "Create Redis credential" }));
  await waitFor(() => expect(within(dialog).getByRole("button", { name: "Create Redis credential" })).toBeEnabled());
  expect(within(dialog).getByLabelText("Password")).toHaveValue("fixture-only-password");
  expect(within(dialog).getByText("Temporary save failure")).toBeVisible();
  await user.click(within(dialog).getByRole("button", { name: "Create Redis credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  await user.click(screen.getByRole("button", { name: "Add credential" }));
  await user.click(screen.getByRole("menuitem", { name: /Redis/ }));
  expect(within(screen.getByRole("dialog")).getByLabelText("Password")).toHaveValue("");
});

it("does not mutate on deletion cancel and delegates confirmed deletion to the selected native profile", async () => {
  const user = userEvent.setup();
  render(<CredentialsPage />);
  const row = (await screen.findByText("Reader")).closest("tr");
  if (!row) throw new Error("Missing Redis row");
  await user.click(within(row).getByRole("button", { name: "Delete credential" }));
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" }));
  expect(apiDelete).not.toHaveBeenCalled();
  await user.click(within(row).getByRole("button", { name: "Delete credential" }));
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiDelete).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles/11");
  expect(loadCredentials).toHaveBeenCalledOnce();
});

it("shows native delete failure only after an attempt and allows retry", async () => {
  vi.mocked(apiDelete).mockRejectedValueOnce(new Error("Temporary delete failure")).mockResolvedValueOnce({});
  const user = userEvent.setup();
  render(<CredentialsPage />);
  const row = (await screen.findByText("Reader")).closest("tr");
  if (!row) throw new Error("Missing Redis row");
  await user.click(within(row).getByRole("button", { name: "Delete credential" }));
  const dialog = screen.getByRole("dialog");
  await user.click(within(dialog).getByRole("button", { name: "Delete credential" }));
  expect(await within(dialog).findByText("Temporary delete failure")).toBeVisible();
  await user.click(within(dialog).getByRole("button", { name: "Delete credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiDelete).toHaveBeenCalledTimes(2);
});

it("blocks native form changes and duplicate submits while the save is unresolved", async () => {
  let finish!: (_value: object) => void;
  vi.mocked(apiPost).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const { user, dialog } = await openCreate("Redis");
  const submit = within(dialog).getByRole("button", { name: "Create Redis credential" });
  await user.click(submit);
  expect(within(dialog).getByRole("textbox", { name: "Username" })).toBeDisabled();
  expect(submit).toBeDisabled();
  await user.click(submit);
  expect(apiPost).toHaveBeenCalledOnce();
  await act(async () => finish({}));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
});

it("shows the empty state after the last native profile is deleted and inventory refresh completes", async () => {
  vi.mocked(useGateway, { partial: true }).mockReturnValue({
    credentials: { state: "ready", data: [], errors: [], error: null },
    loadCredentials,
  });
  vi.mocked(apiGet).mockImplementation(async (path) => {
    if (path === "/api/connectors") return { items: [{ kind: "redis", label: "Redis", version: "0.2" }] };
    if (path === "/api/connector-targets/inventory")
      return {
        items: [{ ...redisTarget, profiles: vi.mocked(apiDelete).mock.calls.length ? [] : redisTarget.profiles }],
      };
    throw new Error(`Unexpected test request: ${path}`);
  });
  const user = userEvent.setup();
  render(<CredentialsPage />);
  await screen.findByText("Reader");
  expect(screen.queryByText("Create your first connector credential.")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Delete credential" }));
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete credential" }));
  expect(await screen.findByText("Create your first connector credential.")).toBeVisible();
  expect(screen.queryByText("Reader")).not.toBeInTheDocument();
});

it.each([false, true])("does not report a failed family as empty and recovers after refresh (retired: %s)", async (retire) => {
  const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
  vi.mocked(useGateway, { partial: true }).mockReturnValue({
    credentials: { state: "ready", data: [], errors: [], error: null },
    loadCredentials,
  });
  vi.mocked(apiGet).mockImplementation(async (path) => {
    const refreshed = vi.mocked(apiPost).mock.calls.length > 0;
    if (path === "/api/connectors")
      return {
        items: [
          { kind: "ssh", label: "SSH", version: "0.2" },
          ...(!refreshed || !retire ? [{ kind: "redis", label: "Redis", version: "0.2" }] : []),
        ],
      };
    if (path === "/api/connector-targets/inventory")
      return { items: [{ ...redisTarget, profiles: [], config: { port: refreshed ? 6379 : [] } }] };
    throw new Error(`Unexpected test request: ${path}`);
  });
  try {
    const user = userEvent.setup();
    render(<CredentialsPage />);
    expect(await screen.findByText(/Connector credentials unavailable: redis/)).toBeVisible();
    expect(screen.queryByText("Create your first connector credential.")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Add credential" }));
    await user.click(screen.getByRole("menuitem", { name: /SSH/ }));
    await user.type(within(screen.getByRole("dialog")).getByRole("textbox", { name: "Name" }), "Test recovery");
    await user.click(screen.getByRole("button", { name: "Generate ed25519 credential" }));
    expect(await screen.findByText("Create your first connector credential.")).toBeVisible();
    expect(screen.queryByText(/Connector credentials unavailable: redis/)).not.toBeInTheDocument();
  } finally {
    consoleError.mockRestore();
  }
});
