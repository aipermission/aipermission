import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { renderCredentialFamily } from "../../../test/render-credential-family";
import { captureCredentialFamily } from "../../editor/capture-credential-family";
import { kafkaCredentialFamily, kafkaCredentialTargets } from "./credential-family";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn() }));
const familyTemplate = kafkaCredentialFamily.create(captureCredentialFamily);
const target = inventoryTargetFixture({
  connector_kind: "kafka",
  name: "Test stream",
  config: { server_family: "redpanda", bootstrap_brokers: "localhost:9092", tls_enabled: true },
  profiles: [
    inventoryProfileFixture({
      connector_kind: "kafka",
      kind: "sasl",
      label: "Reader",
      public: { mechanism: "scram_sha_256", username: "reader" },
    }),
  ],
});
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(apiPost).mockReset().mockResolvedValue({});
  vi.mocked(apiPut).mockReset().mockResolvedValue({});
  vi.mocked(apiDelete).mockReset().mockResolvedValue({});
});

it("creates a native SASL credential with explicit mechanism and encrypted password", async () => {
  const user = userEvent.setup();
  const family = renderCredentialFamily(familyTemplate, { targets: [target] });
  act(() => family.openCreate());
  const dialog = within(screen.getByRole("dialog"));
  await user.selectOptions(dialog.getByRole("combobox", { name: "SASL mechanism" }), "scram_sha_512");
  await user.type(dialog.getByRole("textbox", { name: "Username" }), "writer");
  await user.type(dialog.getByLabelText("Password"), "fixture-only-password");
  await user.click(dialog.getByRole("button", { name: "Create Kafka credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles", {
    kind: "sasl",
    label: "monitor",
    public: { mechanism: "scram_sha_512", username: "writer" },
    secret: { password: "fixture-only-password" },
    risk_label: "stream read",
  });
});

it("preserves a stored SASL password on native metadata editing", async () => {
  const user = userEvent.setup();
  renderCredentialFamily(familyTemplate, { targets: [target] });
  await user.click(screen.getByRole("button", { name: "Edit credential" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByRole("combobox", { name: "SASL mechanism" })).toHaveValue("scram_sha_256");
  expect(dialog.getByLabelText("New password")).not.toBeRequired();
  await user.click(dialog.getByRole("button", { name: "Save Kafka credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles/11", {
    kind: "sasl",
    label: "Reader",
    public: { mechanism: "scram_sha_256", username: "reader" },
    risk_label: "stream read",
  });
});

it("clears encrypted SASL material only when authentication is explicitly disabled", async () => {
  const user = userEvent.setup();
  renderCredentialFamily(familyTemplate, { targets: [target] });
  await user.click(screen.getByRole("button", { name: "Edit credential" }));
  const dialog = within(screen.getByRole("dialog"));
  await user.selectOptions(dialog.getByRole("combobox", { name: "SASL mechanism" }), "none");
  await user.click(dialog.getByRole("button", { name: "Save Kafka credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles/11", {
    kind: "sasl",
    label: "Reader",
    public: { mechanism: "none", username: "" },
    secret: { password: "" },
    risk_label: "stream read",
  });
});

it("validates SASL and TLS metadata before exposing a native form", () => {
  expect(kafkaCredentialTargets([target, inventoryTargetFixture({ config: { tls_enabled: [] } })])).toMatchObject([
    { id: 3, project_id: 7, config: { server_family: "redpanda" }, profiles: [{ id: 11 }] },
  ]);
  expect(() => kafkaCredentialTargets([inventoryTargetFixture({ ...target, config: { tls_enabled: "true" } })])).toThrow("tls_enabled");
  expect(() =>
    kafkaCredentialTargets([inventoryTargetFixture({ ...target, profiles: [inventoryProfileFixture({ public: { mechanism: [] } })] })]),
  ).toThrow("mechanism");
  expect(kafkaCredentialTargets([inventoryTargetFixture({ ...target, config: undefined, profiles: undefined })])[0].profiles).toEqual([]);
});

it.each(["none", "scram_sha_256"])("rotates/enables SASL from %s and deletes the exact selected profile", async (mechanism) => {
  const user = userEvent.setup();
  const selected = inventoryTargetFixture({
    ...target,
    id: 44,
    profiles: [
      inventoryProfileFixture({ connector_kind: "kafka", target_id: 44, id: 22, label: "First stream" }),
      inventoryProfileFixture({
        connector_kind: "kafka",
        target_id: 44,
        id: 77,
        runtime_id: 91,
        kind: "sasl",
        label: "Selected stream",
        public: { mechanism, username: "selected" },
      }),
    ],
  });
  renderCredentialFamily(familyTemplate, { targets: [target, selected] });
  const row = screen.getByText("Selected stream").closest("tr");
  if (!row) throw new Error("Selected native stream credential is missing");
  await user.click(within(row).getByRole("button", { name: "Edit credential" }));
  const dialog = within(screen.getByRole("dialog"));
  await user.selectOptions(dialog.getByRole("combobox", { name: "SASL mechanism" }), "scram_sha_512");
  expect(dialog.getByLabelText("New password").hasAttribute("required")).toBe(mechanism === "none");
  await user.type(dialog.getByLabelText("New password"), "fixture-new-password");
  await user.click(dialog.getByRole("button", { name: "Save Kafka credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/44/profiles/77", {
    kind: "sasl",
    label: "Selected stream",
    public: { mechanism: "scram_sha_512", username: "selected" },
    secret: { password: "fixture-new-password" },
    risk_label: "stream read",
  });
  await user.click(within(row).getByRole("button", { name: "Delete credential" }));
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiDelete).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/44/profiles/77");
});
