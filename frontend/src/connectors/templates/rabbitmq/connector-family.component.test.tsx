import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { renderConnectorFamily } from "../../../test/connector-family-host";
import { captureConnectorFamily } from "../../editor/capture-connector-family";
import { rabbitConnectorFamily } from "./connector-family";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn(), apiGet: vi.fn() }));
const family = rabbitConnectorFamily.create(captureConnectorFamily);
const profile = inventoryProfileFixture({
  connector_kind: "rabbitmq",
  kind: "username_password",
  label: "monitor",
  public: { username: "reader" },
});
const config = { connection_mode: "direct", scheme: "https", host: "queue.example", port: 15671, vhost: "/jobs", transport_target_ref: "" };
const target = inventoryTargetFixture({
  connector_kind: "rabbitmq",
  name: "My queue",
  config,
  profiles: [
    inventoryProfileFixture({
      id: 12,
      connector_kind: "rabbitmq",
      kind: "username_password",
      label: "other",
      public: { username: "other" },
    }),
    profile,
  ],
});
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(apiPost)
    .mockReset()
    .mockResolvedValue({ id: 3, profiles: [{ id: 11 }] });
  vi.mocked(apiPut)
    .mockReset()
    .mockResolvedValue({ id: 3, profiles: [{ id: 11 }] });
  vi.mocked(apiDelete).mockReset().mockResolvedValue({});
});

it("creates RabbitMQ management config and a native username/password profile", async () => {
  const user = userEvent.setup();
  const host = renderConnectorFamily(family);
  act(() => host.commands().openCreate());
  await user.clear(screen.getByLabelText("Management host"));
  await user.type(screen.getByLabelText("Management host"), "queue.example");
  await user.clear(screen.getByLabelText("Management API port"));
  await user.type(screen.getByLabelText("Management API port"), "15671");
  await user.selectOptions(screen.getByLabelText("Scheme"), "https");
  await user.clear(screen.getByLabelText("Default vhost"));
  await user.type(screen.getByLabelText("Default vhost"), "/jobs");
  await user.type(screen.getByLabelText("Username"), "reader");
  await user.type(screen.getByLabelText("Password"), "test-password");
  await user.click(screen.getByRole("button", { name: "Create connector" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/with-profile", {
    target: { connector_kind: "rabbitmq", name: "rabbitmq", project_id: 7, config },
    profile: {
      kind: "username_password",
      label: "monitor",
      public: { username: "reader" },
      secret: { password: "test-password" },
      risk_label: "queue access",
    },
  });
});

it("edits RabbitMQ through its native decoded profile without resetting the password", async () => {
  const user = userEvent.setup();
  const host = renderConnectorFamily(family, { targets: [target] });
  act(() => host.commands().openEdit(target, profile));
  expect(screen.getByLabelText("Username")).toHaveValue("reader");
  expect(screen.getByLabelText("Default vhost")).toHaveValue("/jobs");
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}/with-profile/${profile.id}`, {
    target: { name: "My queue", project_id: 7, config },
    profile: { kind: "username_password", label: "monitor", public: { username: "reader" }, risk_label: "queue access" },
  });
});

it("tests and deletes the local RabbitMQ target using its owned identity", async () => {
  const user = userEvent.setup();
  const host = renderConnectorFamily(family, { targets: [target] });
  vi.mocked(apiPost).mockResolvedValueOnce({ ok: true });
  await act(async () => {
    expect(await host.commands().test(target, profile)).toBe(true);
  });
  expect(apiPost).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}/profiles/${profile.id}/test`, {});
  act(() => host.commands().requestDelete(target));
  await user.click(screen.getByRole("button", { name: "Delete connector" }));
  await waitFor(() => expect(apiDelete).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}`));
});
