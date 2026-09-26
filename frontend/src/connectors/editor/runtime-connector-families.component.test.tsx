import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../test/connector-inventory-fixtures";
import { renderConnectorFamily } from "../../test/connector-family-host";
import { dockerConnectorFamily } from "../templates/docker/connector-family";
import { kubernetesConnectorFamily } from "../templates/kubernetes/connector-family";
import { captureConnectorFamily } from "./capture-connector-family";

vi.mock("../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn(), apiGet: vi.fn() }));
const transport = inventoryTargetFixture({
  id: 9,
  connector_kind: "ssh",
  name: "My host",
  project_id: 7,
  profiles: [inventoryProfileFixture({ id: 5, target_id: 9, connector_kind: "ssh", ref: "ssh:9:5" })],
});
const foreignTransport = inventoryTargetFixture({
  id: 99,
  connector_kind: "ssh",
  name: "Foreign host",
  project_id: 9,
  profiles: [inventoryProfileFixture({ id: 95, target_id: 99, connector_kind: "ssh", ref: "ssh:99:95" })],
});
const cases = [
  {
    kind: "docker",
    name: "docker-host",
    profileLabel: "all-containers",
    profileKind: "container_scope",
    risk: "container access",
    family: dockerConnectorFamily.create(captureConnectorFamily),
    scopeLabel: "Container scope",
    fields: [
      { label: "Allowed containers", value: "api\nweb" },
      { label: "Allowed name patterns", value: "worker-*" },
    ],
    config: { connection_mode: "over_ssh", transport_target_ref: "ssh:9:5", docker_command: "docker" },
    scope: { scope_mode: "selected", allowed_containers: "api\nweb", allowed_patterns: "worker-*" },
  },
  {
    kind: "kubernetes",
    name: "kubernetes",
    profileLabel: "all-namespaces",
    profileKind: "namespace_scope",
    risk: "cluster visibility",
    family: kubernetesConnectorFamily.create(captureConnectorFamily),
    scopeLabel: "Namespace scope",
    fields: [{ label: "Namespaces", value: "production\nmonitoring" }],
    config: {
      connection_mode: "over_ssh",
      transport_target_ref: "ssh:9:5",
      kubectl_command: "kubectl",
      context: "",
      default_namespace: "",
    },
    scope: { scope_mode: "selected", namespaces: "production\nmonitoring" },
  },
];
function setup(entry: (typeof cases)[number]) {
  const first = inventoryProfileFixture({
    id: 40,
    connector_kind: entry.kind,
    kind: entry.profileKind,
    label: "all",
    public: { scope_mode: "all" },
  });
  const selected = inventoryProfileFixture({
    id: 41,
    connector_kind: entry.kind,
    kind: entry.profileKind,
    label: "limited",
    public: entry.scope,
  });
  const target = inventoryTargetFixture({
    connector_kind: entry.kind,
    name: "My runtime",
    config: entry.config,
    profiles: [first, selected],
  });
  return { host: renderConnectorFamily(entry.family, { targets: [transport, foreignTransport, target] }), target, selected };
}
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

it.each(cases)("creates native $kind config and selected scope using the shared editor", async (entry) => {
  const user = userEvent.setup();
  const { host } = setup(entry);
  act(() => host.commands().openCreate());
  expect(screen.getByRole("button", { name: "Create connector" })).toBeDisabled();
  expect(screen.queryByRole("option", { name: /Foreign host/ })).not.toBeInTheDocument();
  await user.selectOptions(screen.getByLabelText("Transport profile"), "ssh:9:5");
  await user.selectOptions(screen.getByLabelText(entry.scopeLabel), "selected");
  for (const field of entry.fields) await user.type(screen.getByLabelText(field.label), field.value);
  await user.click(screen.getByRole("button", { name: "Create connector" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/with-profile", {
    target: { connector_kind: entry.kind, name: entry.name, project_id: 7, config: entry.config },
    profile: { kind: entry.profileKind, label: entry.profileLabel, public: entry.scope, secret: {}, risk_label: entry.risk },
  });
});

it.each(cases)("edits the selected $kind profile without substituting the first profile", async (entry) => {
  const user = userEvent.setup();
  const { host, target, selected } = setup(entry);
  act(() => host.commands().openEdit(target, selected));
  expect(screen.getByLabelText("Profile label")).toHaveValue("limited");
  expect(screen.getByLabelText(entry.scopeLabel)).toHaveValue("selected");
  for (const field of entry.fields) expect(screen.getByLabelText(field.label)).toHaveValue(field.value);
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}/with-profile/${selected.id}`, {
    target: { name: "My runtime", project_id: 7, config: entry.config },
    profile: { kind: entry.profileKind, label: "limited", public: entry.scope, secret: {}, risk_label: entry.risk },
  });
});

it.each(cases)("tests the selected $kind profile and deletes only local metadata", async (entry) => {
  const user = userEvent.setup();
  const { host, target, selected } = setup(entry);
  vi.mocked(apiPost).mockResolvedValueOnce({ ok: true });
  await act(async () => {
    expect(await host.commands().test(target, selected)).toBe(true);
  });
  expect(apiPost).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}/profiles/${selected.id}/test`, {});
  act(() => host.commands().requestDelete(target));
  await user.click(screen.getByRole("button", { name: "Delete connector" }));
  await waitFor(() => expect(apiDelete).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}`));
});
