import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { renderCredentialFamily } from "../../../test/render-credential-family";
import { captureCredentialFamily } from "../../editor/capture-credential-family";
import { clickHouseCredentialFamily, clickHouseCredentialTargets } from "./credential-family";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn() }));
const familyTemplate = clickHouseCredentialFamily.create(captureCredentialFamily);
const target = inventoryTargetFixture({
  connector_kind: "clickhouse",
  name: "Test analytics",
  config: { host: "localhost", port: "9000", database: "default" },
  profiles: [
    inventoryProfileFixture({ connector_kind: "clickhouse", kind: "username_password", label: "Analyst", public: { username: "analyst" } }),
  ],
});
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(apiPost).mockReset().mockResolvedValue({});
  vi.mocked(apiPut).mockReset().mockResolvedValue({});
  vi.mocked(apiDelete).mockReset().mockResolvedValue({});
});

it("supports ClickHouse native creation with its optional empty password", async () => {
  const user = userEvent.setup();
  const family = renderCredentialFamily(familyTemplate, { targets: [target] });
  act(() => family.openCreate());
  const dialog = within(screen.getByRole("dialog"));
  await user.type(dialog.getByRole("textbox", { name: "Username" }), "new_analyst");
  await user.click(dialog.getByRole("button", { name: "Create ClickHouse credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles", {
    kind: "username_password",
    label: "readonly",
    public: { username: "new_analyst" },
    risk_label: "read-only analytics",
    secret: {},
  });
});

it("rotates the selected native profile password without changing its identity", async () => {
  const user = userEvent.setup();
  const selected = inventoryTargetFixture({
    ...target,
    id: 44,
    name: "Other analytics",
    profiles: [
      inventoryProfileFixture({
        connector_kind: "clickhouse",
        target_id: 44,
        id: 22,
        label: "First analyst",
        public: { username: "first" },
      }),
      inventoryProfileFixture({
        connector_kind: "clickhouse",
        target_id: 44,
        id: 77,
        runtime_id: 91,
        kind: "username_password",
        label: "Selected analyst",
        public: { username: "selected" },
      }),
    ],
  });
  renderCredentialFamily(familyTemplate, { targets: [target, selected] });
  const row = screen.getByText("Selected analyst").closest("tr");
  if (!row) throw new Error("Selected native row is missing");
  await user.click(within(row).getByRole("button", { name: "Edit credential" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByRole("textbox", { name: "Username" })).toHaveValue("selected");
  await user.type(dialog.getByLabelText("New password"), "fixture-new-password");
  await user.click(dialog.getByRole("button", { name: "Save ClickHouse credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/44/profiles/77", {
    kind: "username_password",
    label: "Selected analyst",
    public: { username: "selected" },
    risk_label: "read-only analytics",
    secret: { password: "fixture-new-password" },
  });
});

it("uses shared deletion for ClickHouse without managed Postgres role semantics", async () => {
  const user = userEvent.setup();
  renderCredentialFamily(familyTemplate, { targets: [target] });
  await user.click(screen.getByRole("button", { name: "Delete credential" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.queryByRole("button", { name: "Delete role and credential" })).not.toBeInTheDocument();
  await user.click(dialog.getByRole("button", { name: "Delete credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiDelete).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles/11");
});

it("keeps its credential decoder scoped to ClickHouse", () => {
  expect(clickHouseCredentialTargets([target, inventoryTargetFixture({ config: { host: [] } })])).toHaveLength(1);
  expect(() => clickHouseCredentialTargets([inventoryTargetFixture({ ...target, config: { host: [] } })])).toThrow("host");
});
