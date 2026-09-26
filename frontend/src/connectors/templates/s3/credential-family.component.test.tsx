import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { renderCredentialFamily } from "../../../test/render-credential-family";
import { captureCredentialFamily } from "../../editor/capture-credential-family";
import { s3CredentialFamily, s3CredentialTargets } from "./credential-family";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn() }));
const familyTemplate = s3CredentialFamily.create(captureCredentialFamily);
const target = inventoryTargetFixture({
  connector_kind: "s3",
  name: "Test bucket",
  config: { host: "s3.example.test", port: 443, bucket: "test" },
  profiles: [
    inventoryProfileFixture({ connector_kind: "s3", kind: "access_key", label: "Reader", public: { access_key_id: "TEST-ACCESS-KEY" } }),
  ],
});
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(apiPost).mockReset().mockResolvedValue({});
  vi.mocked(apiPut).mockReset().mockResolvedValue({});
  vi.mocked(apiDelete).mockReset().mockResolvedValue({});
});

it("creates the native S3 key and optional session token without exposing secrets in rows", async () => {
  const user = userEvent.setup();
  const family = renderCredentialFamily(familyTemplate, { targets: [target] });
  expect(screen.getByText("access TEST...-KEY")).toBeVisible();
  act(() => family.openCreate());
  const dialog = within(screen.getByRole("dialog"));
  await user.type(dialog.getByRole("textbox", { name: "Access key ID" }), "NEW-TEST-KEY");
  await user.type(dialog.getByLabelText("Secret access key"), "fixture-only-secret");
  await user.type(dialog.getByLabelText("Session token"), "fixture-only-token");
  await user.click(dialog.getByRole("button", { name: "Create S3 credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles", {
    kind: "access_key",
    label: "default",
    public: { access_key_id: "NEW-TEST-KEY" },
    risk_label: "object storage",
    secret: { secret_access_key: "fixture-only-secret", session_token: "fixture-only-token" },
  });
  expect(screen.queryByText("fixture-only-secret")).not.toBeInTheDocument();
});

it("keeps existing S3 secrets on metadata edits and replaces only a supplied token", async () => {
  const user = userEvent.setup();
  renderCredentialFamily(familyTemplate, { targets: [target] });
  await user.click(screen.getByRole("button", { name: "Edit credential" }));
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Save S3 credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/3/profiles/11", {
    kind: "access_key",
    label: "Reader",
    public: { access_key_id: "TEST-ACCESS-KEY" },
    risk_label: "",
  });
  vi.mocked(apiPut).mockClear();
  await user.click(screen.getByRole("button", { name: "Edit credential" }));
  const dialog = within(screen.getByRole("dialog"));
  await user.type(dialog.getByLabelText("New session token"), "fixture-new-token");
  await user.click(dialog.getByRole("button", { name: "Save S3 credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPut).toHaveBeenCalledExactlyOnceWith(
    "/api/connector-targets/3/profiles/11",
    expect.objectContaining({ secret: { session_token: "fixture-new-token" } }),
  );
});

it("deletes only the selected S3 profile without deleting bucket objects", async () => {
  const user = userEvent.setup();
  const selected = inventoryTargetFixture({
    ...target,
    id: 44,
    profiles: [inventoryProfileFixture({ connector_kind: "s3", target_id: 44, id: 77, runtime_id: 91, label: "Other key" })],
  });
  renderCredentialFamily(familyTemplate, { targets: [target, selected] });
  const row = screen.getByText("Other key").closest("tr");
  if (!row) throw new Error("Selected object-store profile is missing");
  await user.click(within(row).getByRole("button", { name: "Delete credential" }));
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiDelete).toHaveBeenCalledExactlyOnceWith("/api/connector-targets/44/profiles/77");
  expect(apiPost).not.toHaveBeenCalled();
});

it("validates S3 native config and public access-key metadata", () => {
  expect(s3CredentialTargets([inventoryTargetFixture({ ...target, config: { port: "443" } })])[0].config.port).toBe("443");
  expect(s3CredentialTargets([target, inventoryTargetFixture({ config: { path_style: [] } })])).toMatchObject([
    { id: 3, project_id: 7, profiles: [{ id: 11 }] },
  ]);
  expect(() => s3CredentialTargets([inventoryTargetFixture({ ...target, config: { path_style: "true" } })])).toThrow("path_style");
  expect(() =>
    s3CredentialTargets([inventoryTargetFixture({ ...target, profiles: [inventoryProfileFixture({ public: { access_key_id: [] } })] })]),
  ).toThrow("access_key_id");
  expect(s3CredentialTargets([inventoryTargetFixture({ ...target, config: undefined, profiles: undefined })])[0].profiles).toEqual([]);
});
