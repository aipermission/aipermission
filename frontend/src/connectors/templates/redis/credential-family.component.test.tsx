import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { renderCredentialFamily } from "../../../test/render-credential-family";
import { captureCredentialFamily } from "../../editor/capture-credential-family";
import { redisCredentialFamily, redisCredentialTargets } from "./credential-family";
import { credentialDisplayRow } from "../_shared/credential-display-row";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn() }));
const familyTemplate = redisCredentialFamily.create(captureCredentialFamily);
const target = inventoryTargetFixture({
  connector_kind: "redis",
  name: "Test cache",
  config: { server_family: "valkey", host: "localhost", port: 6379, database: 2 },
  profiles: [
    inventoryProfileFixture({ connector_kind: "redis", label: "Reader", kind: "username_password", public: { username: "reader" } }),
  ],
});
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(apiPost).mockReset().mockResolvedValue({});
  vi.mocked(apiPut).mockReset().mockResolvedValue({});
  vi.mocked(apiDelete).mockReset().mockResolvedValue({});
});

it("captures the native Redis / Valkey form and creates a profile with the selected target identity", async () => {
  const user = userEvent.setup();
  const family = renderCredentialFamily(familyTemplate, { targets: [target] });
  expect(screen.getByText("Valkey")).toBeVisible();
  act(() => family.openCreate());
  const dialog = screen.getByRole("dialog");
  await user.type(within(dialog).getByRole("textbox", { name: "Username" }), "writer");
  await user.type(within(dialog).getByLabelText("Password"), "fixture-only-password");
  await user.click(within(dialog).getByRole("button", { name: "Create Valkey credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles", {
    kind: "username_password",
    label: "default",
    public: { username: "writer" },
    secret: { password: "fixture-only-password" },
    risk_label: "cache access",
  });
  expect(family.refresh).toHaveBeenCalledOnce();
});

it("loads the native edit row and preserves its secret when only the label changes", async () => {
  const user = userEvent.setup();
  renderCredentialFamily(familyTemplate, { targets: [target] });
  await user.click(screen.getByRole("button", { name: "Edit credential" }));
  const dialog = screen.getByRole("dialog");
  expect(within(dialog).getByRole("textbox", { name: "Username" })).toHaveValue("reader");
  expect(within(dialog).getByLabelText("New password")).toHaveValue("");
  await user.clear(within(dialog).getByRole("textbox", { name: "Profile label" }));
  await user.type(within(dialog).getByRole("textbox", { name: "Profile label" }), "New reader");
  await user.click(within(dialog).getByRole("button", { name: "Save Valkey credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles/11", {
    kind: "username_password",
    label: "New reader",
    public: { username: "reader" },
    risk_label: "",
  });
});

it("deletes the native profile without deleting its connector target", async () => {
  const user = userEvent.setup();
  renderCredentialFamily(familyTemplate, { targets: [target] });
  await user.click(screen.getByRole("button", { name: "Delete credential" }));
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiDelete).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles/11");
});

it("keeps the profile/runtime IDs independent and rejects malformed owned fields", () => {
  const original = inventoryTargetFixture({
    ...target,
    profiles: [inventoryProfileFixture({ connector_kind: "redis", runtime_id: 91, public: { username: "reader" } })],
  });
  const decoded = redisCredentialTargets([original, inventoryTargetFixture({ config: { port: [] } })]);
  expect(decoded).toHaveLength(1);
  expect(decoded[0]).toMatchObject({
    id: 3,
    project_id: 7,
    config: target.config,
    profiles: [{ id: 11, runtime_id: 91, public: { username: "reader" } }],
  });
  expect(decoded[0]).not.toBe(original);
  expect(() => redisCredentialTargets([inventoryTargetFixture({ ...target, config: { database: "not-a-number" } })])).toThrow("database");
  expect(() =>
    redisCredentialTargets([inventoryTargetFixture({ ...target, profiles: [inventoryProfileFixture({ public: { username: [] } })] })]),
  ).toThrow("username");
});

it("projects only public display fields without retaining the native row payload", () => {
  const row = {
    row_id: "example:3:11",
    connector_kind: "example",
    name: "Reader",
    metadata: [undefined, "Public metadata"],
    nativePayload: { privateField: "fixture" },
  };
  expect(credentialDisplayRow(row)).toEqual({
    row_id: "example:3:11",
    connector_kind: "example",
    name: "Reader",
    connector_label: "",
    kind: "",
    target_label: "",
    target_detail: undefined,
    metadata: ["Public metadata"],
    delete_disabled: undefined,
  });
  expect(credentialDisplayRow({ row_id: "example:3:11", connector_kind: "example" }).metadata).toEqual([]);
  expect(row.metadata).toEqual([undefined, "Public metadata"]);
});

it("retains absent optional config and profiles without inventing connection or credential values", () => {
  const [decoded] = redisCredentialTargets([inventoryTargetFixture({ ...target, config: undefined, profiles: undefined })]);
  expect(decoded.id).toBe(3);
  expect(decoded.config?.host).toBeUndefined();
  expect(decoded.config?.database).toBeUndefined();
  expect(decoded.profiles).toEqual([]);
});
