import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { renderConnectorFamily } from "../../../test/connector-family-host";
import { captureConnectorFamily } from "../../editor/capture-connector-family";
import { s3ConnectorFamily } from "./connector-family";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn(), apiGet: vi.fn() }));
const family = s3ConnectorFamily.create(captureConnectorFamily);
const profile = inventoryProfileFixture({ connector_kind: "s3", kind: "access_key", public: { access_key_id: "test-access" } });
const target = inventoryTargetFixture({
  connector_kind: "s3",
  name: "My storage",
  config: { host: "store.example", port: 9443, region: "local", bucket: "my-bucket", path_style: false },
  profiles: [profile],
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

it("creates an S3 profile with native boolean fields and secrets, preserving a selected project", async () => {
  const user = userEvent.setup();
  const host = renderConnectorFamily(family, { projects: [{ id: 8, name: "Other Project" }] });
  act(() => host.commands().openCreate(8));
  await user.type(screen.getByLabelText("Bucket"), "new-bucket");
  await user.type(screen.getByLabelText("Access key ID"), "test-access");
  await user.type(screen.getByLabelText("Secret access key"), "test-secret");
  await user.type(screen.getByLabelText("Session token"), "test-session");
  await user.click(screen.getByLabelText("Verified conditional requests"));
  await user.click(screen.getByRole("button", { name: "Create connector" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/with-profile", {
    target: {
      connector_kind: "s3",
      name: "object-store",
      project_id: 8,
      config: {
        connection_mode: "direct",
        scheme: "https",
        host: "s3.amazonaws.com",
        port: 443,
        region: "us-east-1",
        bucket: "new-bucket",
        path_style: true,
        trust_conditional_requests: true,
        transport_target_ref: "",
      },
    },
    profile: {
      kind: "access_key",
      label: "default",
      public: { access_key_id: "test-access" },
      secret: { secret_access_key: "test-secret", session_token: "test-session" },
      risk_label: "object storage",
    },
  });
  expect(host.props.refresh).toHaveBeenCalledOnce();
});

it("edits S3 config without replacing blank encrypted secrets", async () => {
  const user = userEvent.setup();
  const host = renderConnectorFamily(family, { targets: [target] });
  act(() => host.commands().openEdit(target, profile));
  expect(screen.getByLabelText("Access key ID")).toHaveValue("test-access");
  expect(screen.getByLabelText("Path-style addressing")).not.toBeChecked();
  await user.clear(screen.getByLabelText("Port"));
  await user.type(screen.getByLabelText("Port"), "8443");
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}/with-profile/${profile.id}`, {
    target: {
      name: "My storage",
      project_id: 7,
      config: {
        connection_mode: "direct",
        scheme: "https",
        host: "store.example",
        port: 8443,
        region: "local",
        bucket: "my-bucket",
        path_style: false,
        trust_conditional_requests: false,
        transport_target_ref: "",
      },
    },
    profile: { kind: "access_key", label: profile.label, public: { access_key_id: "test-access" }, risk_label: "object storage" },
  });
});

it("tests the selected S3 profile and deletes only its local target", async () => {
  const user = userEvent.setup();
  const host = renderConnectorFamily(family, { targets: [target] });
  vi.mocked(apiPost).mockResolvedValueOnce({ ok: true });
  await act(async () => {
    expect(await host.commands().test(target, profile)).toBe(true);
  });
  expect(apiPost).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}/profiles/${profile.id}/test`, {});
  act(() => host.commands().requestDelete(target));
  expect(screen.getByText(/It does not delete buckets or objects/)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Delete connector" }));
  await waitFor(() => expect(apiDelete).toHaveBeenCalledExactlyOnceWith(`/api/connector-targets/${target.id}`));
});
