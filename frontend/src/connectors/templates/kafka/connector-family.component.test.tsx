import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { renderConnectorFamily } from "../../../test/connector-family-host";
import { captureConnectorFamily } from "../../editor/capture-connector-family";
import { kafkaConnectorFamily } from "./connector-family";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn(), apiGet: vi.fn() }));
const family = kafkaConnectorFamily.create(captureConnectorFamily);
const profile = inventoryProfileFixture({
  connector_kind: "kafka",
  kind: "sasl",
  label: "monitor",
  public: { mechanism: "scram_sha_256", username: "reader" },
});
const config = {
  server_family: "redpanda",
  connection_mode: "direct",
  bootstrap_brokers: "broker.example:9093",
  transport_target_ref: "",
  tls_enabled: true,
  allow_insecure_plain_sasl: false,
  tls_server_name: "broker.example",
  tls_ca_pem: "test-ca",
};
const target = inventoryTargetFixture({
  connector_kind: "kafka",
  name: "My stream",
  config,
  profiles: [
    inventoryProfileFixture({ id: 12, connector_kind: "kafka", kind: "sasl", label: "other", public: { mechanism: "none" } }),
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

it("creates Redpanda with native TLS and SASL settings through its captured editor", async () => {
  const user = userEvent.setup();
  const host = renderConnectorFamily(family);
  act(() => host.commands().openCreate());
  await user.selectOptions(screen.getByLabelText("Server family"), "redpanda");
  await user.clear(screen.getByLabelText("Bootstrap brokers"));
  await user.type(screen.getByLabelText("Bootstrap brokers"), "broker.example:9093");
  await user.selectOptions(screen.getByLabelText("TLS", { selector: "select" }), "enabled");
  await user.type(screen.getByLabelText("TLS server name"), "broker.example");
  await user.type(screen.getByLabelText("Custom CA certificate"), "test-ca");
  await user.selectOptions(screen.getByLabelText("SASL mechanism"), "scram_sha_256");
  await user.type(screen.getByLabelText("Username"), "reader");
  await user.type(screen.getByLabelText("Password"), "test-password");
  await user.click(screen.getByRole("button", { name: "Create connector" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/with-profile", {
    target: { connector_kind: "kafka", name: "event-stream", project_id: 7, config },
    profile: {
      kind: "sasl",
      label: "monitor",
      public: { mechanism: "scram_sha_256", username: "reader" },
      secret: { password: "test-password" },
      risk_label: "stream read",
    },
  });
});

it("preserves existing SASL secrets and clears disabled native TLS config on edit", async () => {
  const user = userEvent.setup();
  const host = renderConnectorFamily(family, { targets: [target] });
  act(() => host.commands().openEdit(target, profile));
  expect(screen.getByLabelText("Server family")).toHaveValue("redpanda");
  expect(screen.getByLabelText("SASL mechanism")).toHaveValue("scram_sha_256");
  expect(screen.getByLabelText("Username")).toHaveValue("reader");
  await user.selectOptions(screen.getByLabelText("TLS", { selector: "select" }), "disabled");
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}/with-profile/${profile.id}`, {
    target: { name: "My stream", project_id: 7, config: { ...config, tls_enabled: false, tls_server_name: "", tls_ca_pem: "" } },
    profile: { kind: "sasl", label: "monitor", public: { mechanism: "scram_sha_256", username: "reader" }, risk_label: "stream read" },
  });
});

it("tests the selected Kafka profile and deletes local configuration without changing the cluster", async () => {
  const user = userEvent.setup();
  const host = renderConnectorFamily(family, { targets: [target] });
  vi.mocked(apiPost).mockResolvedValueOnce({ ok: true });
  await act(async () => {
    expect(await host.commands().test(target, profile)).toBe(true);
  });
  expect(apiPost).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}/profiles/${profile.id}/test`, {});
  act(() => host.commands().requestDelete(target));
  expect(screen.getByText(/does not change the Kafka or Redpanda cluster/)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Delete connector" }));
  await waitFor(() => expect(apiDelete).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}`));
});
