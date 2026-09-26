import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { renderCredentialFamily } from "../../../test/render-credential-family";
import { captureCredentialFamily } from "../../editor/capture-credential-family";
import { dockerCredentialFamily, dockerCredentialTargets } from "./credential-family";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn() }));
const familyTemplate = dockerCredentialFamily.create(captureCredentialFamily);
const target = inventoryTargetFixture({
  connector_kind: "docker",
  name: "Test containers",
  config: { transport_target_ref: "transport:8:9", docker_command: "docker" },
  profiles: [
    inventoryProfileFixture({
      connector_kind: "docker",
      kind: "container_scope",
      label: "Selected API",
      public: { scope_mode: "selected", allowed_containers: "api", allowed_patterns: "project-*" },
    }),
  ],
});
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(apiPost).mockReset().mockResolvedValue({});
  vi.mocked(apiPut).mockReset().mockResolvedValue({});
  vi.mocked(apiDelete).mockReset().mockResolvedValue({});
});

it("creates a native selected-container scope without runtime or secret material", async () => {
  const user = userEvent.setup();
  const family = renderCredentialFamily(familyTemplate, { targets: [target] });
  act(() => family.openCreate());
  const dialog = within(screen.getByRole("dialog"));
  await user.selectOptions(dialog.getByRole("combobox", { name: "Container scope" }), "selected");
  await user.type(dialog.getByRole("textbox", { name: "Allowed containers" }), "test-api\ntest-web");
  await user.type(dialog.getByRole("textbox", { name: "Allowed name patterns" }), "test-worker-*");
  await user.click(dialog.getByRole("button", { name: "Create Docker scope" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles", {
    kind: "container_scope",
    label: "all-containers",
    public: { scope_mode: "selected", allowed_containers: "test-api\ntest-web", allowed_patterns: "test-worker-*" },
    secret: {},
    risk_label: "container access",
  });
});

it("edits native Docker allowlists rather than using a shared credential form", async () => {
  const user = userEvent.setup();
  renderCredentialFamily(familyTemplate, { targets: [target] });
  await user.click(screen.getByRole("button", { name: "Edit credential" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByRole("textbox", { name: "Allowed containers" })).toHaveValue("api");
  expect(dialog.getByRole("textbox", { name: "Allowed name patterns" })).toHaveValue("project-*");
  await user.clear(dialog.getByRole("textbox", { name: "Allowed containers" }));
  await user.type(dialog.getByRole("textbox", { name: "Allowed containers" }), "test-api");
  await user.click(dialog.getByRole("button", { name: "Save Docker scope" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles/11", {
    kind: "container_scope",
    label: "Selected API",
    public: { scope_mode: "selected", allowed_containers: "test-api", allowed_patterns: "project-*" },
    secret: {},
    risk_label: "",
  });
});

it("deletes the exact selected scope without invoking container lifecycle operations", async () => {
  const user = userEvent.setup();
  const selected = inventoryTargetFixture({
    ...target,
    id: 44,
    profiles: [inventoryProfileFixture({ connector_kind: "docker", target_id: 44, id: 77, runtime_id: 91, label: "Other scope" })],
  });
  renderCredentialFamily(familyTemplate, { targets: [target, selected] });
  const row = screen.getByText("Other scope").closest("tr");
  if (!row) throw new Error("Selected container scope is missing");
  await user.click(within(row).getByRole("button", { name: "Delete credential" }));
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiDelete).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/44/profiles/77");
  expect(apiPost).not.toHaveBeenCalled();
});

it("rejects malformed native scope fields and keeps transport identity separate", () => {
  expect(dockerCredentialTargets([target, inventoryTargetFixture({ config: { docker_command: [] } })])).toMatchObject([
    { id: 3, project_id: 7, config: { transport_target_ref: "transport:8:9" }, profiles: [{ id: 11 }] },
  ]);
  expect(() => dockerCredentialTargets([inventoryTargetFixture({ ...target, config: { docker_command: [] } })])).toThrow("docker_command");
  expect(() =>
    dockerCredentialTargets([
      inventoryTargetFixture({ ...target, profiles: [inventoryProfileFixture({ public: { allowed_containers: [] } })] }),
    ]),
  ).toThrow("allowed_containers");
  expect(dockerCredentialTargets([inventoryTargetFixture({ ...target, config: undefined, profiles: undefined })])[0].profiles).toEqual([]);
});
