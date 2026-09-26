import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { renderCredentialFamily } from "../../../test/render-credential-family";
import { captureCredentialFamily } from "../../editor/capture-credential-family";
import { kubernetesCredentialFamily, kubernetesCredentialTargets } from "./credential-family";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn() }));
const familyTemplate = kubernetesCredentialFamily.create(captureCredentialFamily);
const target = inventoryTargetFixture({
  connector_kind: "kubernetes",
  name: "Test cluster",
  config: { transport_target_ref: "transport:8:9", kubectl_command: "kubectl", context: "test" },
  profiles: [
    inventoryProfileFixture({
      connector_kind: "kubernetes",
      kind: "namespace_scope",
      label: "Reader",
      public: { scope_mode: "selected", namespaces: "test\nmonitoring" },
    }),
  ],
});
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(apiPost).mockReset().mockResolvedValue({});
  vi.mocked(apiPut).mockReset().mockResolvedValue({});
  vi.mocked(apiDelete).mockReset().mockResolvedValue({});
});

it("creates a native namespace allowlist without changing the remote kubectl environment", async () => {
  const user = userEvent.setup();
  const family = renderCredentialFamily(familyTemplate, { targets: [target] });
  act(() => family.openCreate());
  const dialog = within(screen.getByRole("dialog"));
  await user.selectOptions(dialog.getByRole("combobox", { name: "Namespace scope" }), "selected");
  await user.type(dialog.getByRole("textbox", { name: "Namespaces" }), "test\nmonitoring");
  await user.click(dialog.getByRole("button", { name: "Create namespace scope" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles", {
    kind: "namespace_scope",
    label: "all-namespaces",
    public: { scope_mode: "selected", namespaces: "test\nmonitoring" },
    secret: {},
    risk_label: "cluster visibility",
  });
});

it("retains the native selected namespace fields on edit", async () => {
  const user = userEvent.setup();
  renderCredentialFamily(familyTemplate, { targets: [target] });
  await user.click(screen.getByRole("button", { name: "Edit credential" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByRole("textbox", { name: "Namespaces" })).toHaveValue("test\nmonitoring");
  await user.clear(dialog.getByRole("textbox", { name: "Namespaces" }));
  await user.type(dialog.getByRole("textbox", { name: "Namespaces" }), "test");
  await user.click(dialog.getByRole("button", { name: "Save namespace scope" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles/11", {
    kind: "namespace_scope",
    label: "Reader",
    public: { scope_mode: "selected", namespaces: "test" },
    secret: {},
    risk_label: "",
  });
});

it("deletes only the selected local namespace scope", async () => {
  const user = userEvent.setup();
  const selected = inventoryTargetFixture({
    ...target,
    id: 44,
    profiles: [
      inventoryProfileFixture({ connector_kind: "kubernetes", target_id: 44, id: 77, runtime_id: 91, label: "Other namespace scope" }),
    ],
  });
  renderCredentialFamily(familyTemplate, { targets: [target, selected] });
  const row = screen.getByText("Other namespace scope").closest("tr");
  if (!row) throw new Error("Selected namespace scope is missing");
  await user.click(within(row).getByRole("button", { name: "Delete credential" }));
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiDelete).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/44/profiles/77");
  expect(apiPost).not.toHaveBeenCalled();
});

it("validates Kubernetes configuration and native namespace public metadata", () => {
  expect(kubernetesCredentialTargets([target, inventoryTargetFixture({ config: { context: [] } })])).toMatchObject([
    { id: 3, project_id: 7, config: { transport_target_ref: "transport:8:9" }, profiles: [{ id: 11 }] },
  ]);
  expect(() => kubernetesCredentialTargets([inventoryTargetFixture({ ...target, config: { context: [] } })])).toThrow("context");
  expect(() =>
    kubernetesCredentialTargets([
      inventoryTargetFixture({ ...target, profiles: [inventoryProfileFixture({ public: { namespaces: [] } })] }),
    ]),
  ).toThrow("namespaces");
  expect(kubernetesCredentialTargets([inventoryTargetFixture({ ...target, config: undefined, profiles: undefined })])[0].profiles).toEqual(
    [],
  );
});
