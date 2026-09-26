import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { renderCredentialFamily } from "../../../test/render-credential-family";
import { captureCredentialFamily } from "../../editor/capture-credential-family";
import { rabbitCredentialFamily, rabbitCredentialTargets } from "./credential-family";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn() }));
const familyTemplate = rabbitCredentialFamily.create(captureCredentialFamily);
const target = inventoryTargetFixture({
  connector_kind: "rabbitmq",
  name: "Test queue",
  config: { host: "localhost", port: 15672, vhost: "/test" },
  profiles: [
    inventoryProfileFixture({ connector_kind: "rabbitmq", kind: "username_password", label: "Monitor", public: { username: "monitor" } }),
  ],
});
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(apiPost).mockReset().mockResolvedValue({});
  vi.mocked(apiPut).mockReset().mockResolvedValue({});
  vi.mocked(apiDelete).mockReset().mockResolvedValue({});
});

it("uses the native RabbitMQ form for a credential-only create", async () => {
  const user = userEvent.setup();
  const family = renderCredentialFamily(familyTemplate, { targets: [target] });
  act(() => family.openCreate());
  const dialog = within(screen.getByRole("dialog"));
  await user.type(dialog.getByRole("textbox", { name: "Username" }), "writer");
  await user.type(dialog.getByLabelText("Password"), "fixture-only-password");
  await user.click(dialog.getByRole("button", { name: "Create RabbitMQ credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles", {
    kind: "username_password",
    label: "monitor",
    public: { username: "writer" },
    secret: { password: "fixture-only-password" },
    risk_label: "queue access",
  });
});

it("preserves the stored RabbitMQ secret on metadata-only editing", async () => {
  const user = userEvent.setup();
  renderCredentialFamily(familyTemplate, { targets: [target] });
  await user.click(screen.getByRole("button", { name: "Edit credential" }));
  const dialog = within(screen.getByRole("dialog"));
  await user.clear(dialog.getByRole("textbox", { name: "Profile label" }));
  await user.type(dialog.getByRole("textbox", { name: "Profile label" }), "Renamed monitor");
  await user.click(dialog.getByRole("button", { name: "Save RabbitMQ credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles/11", {
    kind: "username_password",
    label: "Renamed monitor",
    public: { username: "monitor" },
    risk_label: "",
  });
});

it("deletes the selected RabbitMQ credential without affecting queues", async () => {
  const user = userEvent.setup();
  const selected = inventoryTargetFixture({
    ...target,
    id: 44,
    profiles: [inventoryProfileFixture({ connector_kind: "rabbitmq", target_id: 44, id: 77, runtime_id: 91, label: "Other monitor" })],
  });
  renderCredentialFamily(familyTemplate, { targets: [target, selected] });
  const row = screen.getByText("Other monitor").closest("tr");
  if (!row) throw new Error("Selected queue credential is missing");
  await user.click(within(row).getByRole("button", { name: "Delete credential" }));
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiDelete).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/44/profiles/77");
  expect(apiPost).not.toHaveBeenCalled();
});

it("validates RabbitMQ connection fields and retains independent identities", () => {
  expect(rabbitCredentialTargets([target, inventoryTargetFixture({ config: { port: [] } })])).toMatchObject([
    { id: 3, project_id: 7, profiles: [{ id: 11 }] },
  ]);
  expect(() => rabbitCredentialTargets([inventoryTargetFixture({ ...target, config: { port: "15672" } })])).toThrow("port");
  expect(() =>
    rabbitCredentialTargets([inventoryTargetFixture({ ...target, profiles: [inventoryProfileFixture({ public: { username: [] } })] })]),
  ).toThrow("username");
  expect(rabbitCredentialTargets([inventoryTargetFixture({ ...target, config: undefined, profiles: undefined })])[0].profiles).toEqual([]);
});
