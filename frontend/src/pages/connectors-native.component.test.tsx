import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiGet, apiPost, apiPut } from "../lib/api";
import { useGateway } from "../lib/gateway-context";
import { inventoryProfileFixture, inventoryTargetFixture } from "../test/connector-inventory-fixtures";
import { ConnectorsPage } from "./connectors";

vi.mock("../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn() }));
vi.mock("../lib/gateway-context", () => ({ useGateway: vi.fn() }));
const catalog = [
  { kind: "redis", label: "Redis", version: "0.2" },
  { kind: "s3", label: "S3", version: "0.2" },
  { kind: "postgres", label: "Postgres", version: "0.2" },
];
const reader = inventoryProfileFixture({
  connector_kind: "redis",
  kind: "username_password",
  public: { username: "reader" },
  label: "Reader",
  ref: "redis:3:11",
});
const writer = inventoryProfileFixture({
  id: 22,
  connector_kind: "redis",
  kind: "username_password",
  public: { username: "writer" },
  label: "Writer",
  ref: "redis:3:22",
});
const cache = inventoryTargetFixture({
  connector_kind: "redis",
  name: "Test cache",
  config: { host: "localhost", port: 6379 },
  profiles: [reader, writer],
});
const database = inventoryTargetFixture({
  id: 4,
  connector_kind: "postgres",
  name: "Test database",
  config: { host: "localhost", port: 5432, database: "test" },
  profiles: [
    inventoryProfileFixture({
      id: 41,
      target_id: 4,
      connector_kind: "postgres",
      kind: "username_password",
      public: { username: "admin" },
      ref: "postgres:4:41",
    }),
  ],
});
const loadTargets = vi.fn(async () => []);
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(apiGet).mockImplementation(async (path) => {
    if (path === "/api/connectors") return { items: catalog };
    const detail = catalog.find((item) => path === `/api/connectors/${item.kind}`);
    if (detail) return detail;
    if (path === "/api/connector-targets/inventory") return { items: [cache, database] };
    if (path === "/api/projects")
      return {
        items: [
          { id: 7, name: "My Project", slug: "ungrouped", target_count: 2 },
          { id: 8, name: "Other Project", slug: "other", target_count: 0 },
        ],
      };
    throw new Error(`Unexpected test request: ${path}`);
  });
  vi.mocked(apiPost)
    .mockReset()
    .mockResolvedValue({ id: 3, profiles: [{ id: 11 }], ok: true });
  vi.mocked(apiPut)
    .mockReset()
    .mockResolvedValue({ id: 3, profiles: [{ id: 11 }] });
  vi.mocked(apiDelete).mockReset().mockResolvedValue({});
  vi.mocked(useGateway, { partial: true }).mockReturnValue({
    credentials: { state: "ready", data: [], errors: [], error: null },
    targets: { state: "ready", data: [], error: null },
    loadTargets,
  });
});

async function mount() {
  const user = userEvent.setup();
  render(
    <MemoryRouter>
      <ConnectorsPage />
    </MemoryRouter>,
  );
  await screen.findByText("Test cache");
  await waitFor(() => expect(screen.getByRole("combobox", { name: "Credential profile for Test cache" })).toHaveValue("11"));
  return user;
}
async function cacheRow() {
  const row = (await screen.findByText("Test cache")).closest("tr");
  if (!row) throw new Error("Missing cache row");
  return within(row);
}

it("creates through the real native family and refreshes the inventory owner", async () => {
  const user = await mount();
  await user.click(screen.getByRole("button", { name: "Add connector" }));
  await user.click(screen.getByRole("menuitem", { name: /Redis/ }));
  await user.type(screen.getByLabelText("Password"), "fixture-only-password");
  await user.click(screen.getByRole("button", { name: "Create connector" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith(
    "/api/connector-targets/with-profile",
    expect.objectContaining({
      target: expect.objectContaining({ connector_kind: "redis", project_id: 7 }),
      profile: expect.objectContaining({ secret: { password: "fixture-only-password" } }),
    }),
  );
  expect(screen.getByText("Connector created.")).toBeVisible();
  expect(loadTargets).toHaveBeenCalledTimes(2);
});

it("preserves the chosen project but retires native secrets when switching connector types", async () => {
  const user = await mount();
  await user.click(screen.getByRole("button", { name: "Add connector" }));
  await user.click(screen.getByRole("menuitem", { name: /Redis/ }));
  await user.selectOptions(screen.getByLabelText("Project"), "8");
  await user.type(screen.getByLabelText("Password"), "fixture-only-password");
  await user.selectOptions(screen.getByLabelText("Connector type"), "s3");
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
  expect(screen.getByLabelText("Project")).toHaveValue("8");
  expect(screen.getByLabelText("Secret access key")).toHaveValue("");
  await user.selectOptions(screen.getByLabelText("Connector type"), "redis");
  expect(screen.getByLabelText("Password")).toHaveValue("");
  expect(screen.getByLabelText("Project")).toHaveValue("8");
  expect(apiPost).not.toHaveBeenCalled();
});

it("edits the second profile through the matching native command", async () => {
  const user = await mount();
  const row = await cacheRow();
  await user.selectOptions(row.getByRole("combobox"), "22");
  await user.click(row.getByRole("button", { name: "Edit connector" }));
  expect(screen.getByLabelText("Username")).toHaveValue("writer");
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith(
    "/api/connector-targets/3/with-profile/22",
    expect.objectContaining({
      profile: expect.objectContaining({ public: { username: "writer" } }),
    }),
  );
});

it("merges native connection-test status into the selected inventory row", async () => {
  const user = await mount();
  const row = await cacheRow();
  await user.selectOptions(row.getByRole("combobox"), "22");
  vi.mocked(apiPost).mockResolvedValueOnce({ ok: true, duration_ms: 3 });
  await user.click(row.getByRole("button", { name: "Test connection" }));
  expect(await row.findByText("3ms")).toBeVisible();
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles/22/test", {});
});

it("cancels and confirms native deletion without handing operations to the page", async () => {
  const user = await mount();
  const row = await cacheRow();
  await user.click(row.getByRole("button", { name: "Delete connector" }));
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(apiDelete).not.toHaveBeenCalled();
  await user.click(row.getByRole("button", { name: "Delete connector" }));
  await user.click(screen.getByRole("button", { name: "Delete connector" }));
  await waitFor(() => expect(apiDelete).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3"));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
});

it("keeps connector-owned operations available in the generic inventory table", async () => {
  const user = await mount();
  await user.click(screen.getByRole("button", { name: "Backup / restore database" }));
  expect(await screen.findByRole("dialog", { name: "Test database backup / restore" })).toBeVisible();
  expect(apiPost).not.toHaveBeenCalled();
});

it.each(["mail", "kafka"])("renders and dispatches %s inventory controls through its native family", async (kind) => {
  const entry = { kind, label: kind, version: "0.2" };
  const profile = inventoryProfileFixture({
    id: 70,
    target_id: 7,
    connector_kind: kind,
    ref: `${kind}:7:70`,
    public:
      kind === "mail" ? { imap_enabled: true, smtp_auth_mode: "disabled", mailbox_address: "support@example.com" } : { mechanism: "none" },
  });
  const target = inventoryTargetFixture({
    id: 7,
    connector_kind: kind,
    name: `Test ${kind}`,
    config: kind === "mail" ? { imap_host: "imap.example.com", imap_port: 993 } : { bootstrap_brokers: "localhost:9092" },
    profiles: [profile],
  });
  const get = vi.mocked(apiGet).getMockImplementation();
  if (!get) throw new Error("Missing inventory fixture");
  vi.mocked(apiGet).mockImplementation((path, options) => {
    if (path === "/api/connectors") return Promise.resolve({ items: [...catalog, entry] });
    if (path === `/api/connectors/${kind}`) return Promise.resolve(entry);
    if (path === "/api/connector-targets/inventory") return Promise.resolve({ items: [cache, database, target] });
    return get(path, options);
  });
  const user = await mount();
  const element = screen.getByText(`Test ${kind}`).closest("tr");
  if (!element) throw new Error("Missing native connector row");
  const row = within(element);
  expect(row.getAllByRole("button")).toHaveLength(3);
  await user.click(row.getByRole("button", { name: "Edit connector" }));
  expect(screen.getByRole("button", { name: "Save changes" })).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  vi.mocked(apiPost).mockResolvedValueOnce({ ok: true, duration_ms: 4 });
  await user.click(row.getByRole("button", { name: "Test connection" }));
  expect(await row.findByText("4ms")).toBeVisible();
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/7/profiles/70/test", {});
  await user.click(row.getByRole("button", { name: "Delete connector" }));
  expect(screen.getByRole("dialog")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(apiPut).not.toHaveBeenCalled();
  expect(apiDelete).not.toHaveBeenCalled();
});

it.each(["/api/connectors/redis", "/api/connectors"])("keeps existing native controls working when %s cannot load", async (failedPath) => {
  const get = vi.mocked(apiGet).getMockImplementation();
  if (!get) throw new Error("Missing inventory fixture");
  vi.mocked(apiGet).mockImplementation((path, options) =>
    path === failedPath ? Promise.reject(new Error("Catalog unavailable")) : get(path, options),
  );
  const user = await mount();
  await screen.findByText(/Catalog unavailable/);
  const row = await cacheRow();
  await user.click(row.getByRole("button", { name: "Edit connector" }));
  expect(screen.getByLabelText("Username")).toHaveValue("reader");
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  vi.mocked(apiPost).mockResolvedValueOnce({ ok: true, duration_ms: 3 });
  await user.click(row.getByRole("button", { name: "Test connection" }));
  expect(await row.findByText("3ms")).toBeVisible();
  await user.click(row.getByRole("button", { name: "Delete connector" }));
  expect(screen.getByRole("dialog")).toBeVisible();
  expect(apiDelete).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  await user.click(screen.getByRole("button", { name: "Add connector" }));
  expect(screen.queryByRole("menuitem", { name: /Redis/ })).not.toBeInTheDocument();
});
