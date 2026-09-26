import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ComponentProps } from "react";
import { beforeEach, expect, it, vi } from "vitest";
import { S3ConnectorConsoleTemplate } from "./console";
import { gatewayTargetFixture } from "../../../test/connector-inventory-fixtures";
import { apiPost } from "../../../lib/api";
import { connectorActionFixture, connectorActionRequest } from "../../../test/connector-action-fixtures";

vi.mock("../../../lib/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../../lib/api")>()),
  apiPost: vi.fn(),
}));

beforeEach(() => {
  vi.mocked(apiPost)
    .mockReset()
    .mockImplementation(async (_path, payload) => {
      const request = connectorActionRequest(payload);
      const input = request.input;
      let output: unknown;
      if (request.action_name === "list_objects") {
        const keys = input.prefix ? ["docs/note.txt"] : input.cursor ? ["beta.txt"] : ["alpha.txt"];
        output = {
          objects: keys
            .filter((key) => !input.search || key.includes(String(input.search)))
            .map((key) => ({ key, size: 12, etag: "etag-current" })),
          directories: input.prefix || input.search || input.cursor ? [] : [{ prefix: "docs/", name: "docs" }],
          next_cursor: input.prefix || input.cursor || input.search ? "" : "page-2",
        };
      } else if (request.action_name === "get_object_metadata") output = { key: input.key, etag: "etag-current", size: 12 };
      else if (request.action_name === "bucket_info") output = { bucket: "test-bucket" };
      else if (request.action_name === "list_object_versions") output = { versions: [{ version_id: "v1", size: 12 }], next_cursor: "" };
      else if (request.action_name === "get_bucket_lifecycle") output = { rules: [] };
      else throw new Error(`Unexpected S3 action ${request.action_name}`);
      return connectorActionFixture({
        target_ref: request.target_ref,
        connector_kind: "s3",
        action_name: request.action_name,
        output,
      });
    });
});

function renderActiveConsole() {
  const props: ComponentProps<typeof S3ConnectorConsoleTemplate> = {
    target: gatewayTargetFixture({
      ref: "s3:1:1",
      connector_kind: "s3",
      name: "objects",
      transfer_runtime_id: 7,
      config: { bucket: "test-bucket" },
    }),
    approvals: { state: "ready", data: [], error: null },
    theme: "dark",
    session: { active: true, startedAt: "s3-session" },
    onNewStructuredSession: vi.fn(),
    onRefreshActivity: vi.fn(),
  };
  return { props, ...render(<S3ConnectorConsoleTemplate {...props} />) };
}

it("starts an inactive S3 console through the structured session control", async () => {
  const onNewStructuredSession = vi.fn();
  render(
    <S3ConnectorConsoleTemplate
      target={gatewayTargetFixture({ ref: "s3:1:1", connector_kind: "s3", name: "objects", config: { bucket: "test-bucket" } })}
      approvals={{ state: "ready", data: [], error: null }}
      theme="dark"
      session={{ active: false, startedAt: "" }}
      onNewStructuredSession={onNewStructuredSession}
      onRefreshActivity={vi.fn()}
    />,
  );
  expect(screen.getByText("No active S3 session")).toBeInTheDocument();
  await userEvent.setup().click(screen.getByRole("button", { name: "Start S3 session" }));
  expect(onNewStructuredSession).toHaveBeenCalledOnce();
});

it("does not interpret a live console session as an active structured object browser", () => {
  render(
    <S3ConnectorConsoleTemplate
      target={gatewayTargetFixture({ ref: "s3:1:1", connector_kind: "s3", config: { bucket: "test-bucket" } })}
      approvals={{ state: "ready", data: [], error: null }}
      theme="dark"
      session={{ id: 9, status: "live" }}
      onNewStructuredSession={vi.fn()}
      onRefreshActivity={vi.fn()}
    />,
  );
  expect(screen.getByText("No active S3 session")).toBeInTheDocument();
});

it("connects native object browsing, pagination and metadata through the shared action runner", async () => {
  const user = userEvent.setup();
  renderActiveConsole();
  await screen.findByTitle("alpha.txt");
  await user.click(screen.getByRole("button", { name: "Load more" }));
  await screen.findByTitle("beta.txt");
  expect(screen.getByTitle("alpha.txt")).toBeVisible();
  await user.click(screen.getByTitle("alpha.txt"));
  expect(screen.getByTitle("alpha.txt").closest("button")).toHaveAttribute("aria-pressed", "true");
  await waitFor(() => expect(screen.getByTitle("Object versions")).toBeEnabled());
  expect(apiPost).toHaveBeenLastCalledWith(
    "/api/connector-actions/local-run",
    expect.objectContaining({
      target_ref: "s3:1:1",
      action_name: "get_object_metadata",
      input: { key: "alpha.txt" },
    }),
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
  await user.click(screen.getByTitle("alpha.txt"));
  expect(screen.getByTitle("Object versions")).toBeDisabled();
  await user.click(screen.getByTitle("docs/"));
  await screen.findByTitle("docs/note.txt");
  expect(screen.queryByTitle("alpha.txt")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: /bucket root/ }));
  await screen.findByTitle("alpha.txt");
  await user.type(screen.getByPlaceholderText("Search object keys"), "alpha");
  await user.click(screen.getByRole("button", { name: "Search" }));
  await waitFor(() => expect(screen.queryByTitle("docs/")).not.toBeInTheDocument());
  expect(screen.getByTitle("alpha.txt")).toBeVisible();
  const listing = { action_name: "list_objects", input: { prefix: "", search: "alpha", cursor: "", limit: 100 } };
  expect(apiPost).toHaveBeenLastCalledWith(expect.anything(), expect.objectContaining(listing), expect.anything());
  const previousCalls = vi.mocked(apiPost).mock.calls.length;
  await user.click(screen.getByTitle("Refresh objects"));
  await waitFor(() => expect(screen.getByTitle("Refresh objects")).toBeEnabled());
  expect(apiPost).toHaveBeenCalledTimes(previousCalls + 1);
  expect(apiPost).toHaveBeenLastCalledWith(expect.anything(), expect.objectContaining(listing), expect.anything());
  expect(screen.getByTitle("alpha.txt")).toBeVisible();
  await user.click(screen.getByTitle("Bucket info"));
  expect(apiPost).toHaveBeenLastCalledWith(expect.anything(), expect.objectContaining({ action_name: "bucket_info" }), expect.anything());
});

it("opens native object dialogs and resets them when the structured session ends", async () => {
  const user = userEvent.setup();
  const { rerender, props } = renderActiveConsole();
  await user.click(await screen.findByTitle("alpha.txt"));
  await waitFor(() => expect(screen.getByTitle("Object versions")).toBeEnabled());
  await user.click(screen.getByTitle("Object versions"));
  const versions = await screen.findByRole("dialog", { name: "S3 object versions" });
  await within(versions).findByTitle("v1");
  await user.click(within(versions).getByRole("button", { name: "Close" }));
  await user.click(screen.getByTitle("Create temporary S3 URL"));
  const presign = screen.getByRole("dialog", { name: "Create temporary S3 URL" });
  expect(within(presign).getByRole("textbox", { name: "Object key" })).toHaveValue("alpha.txt");
  await user.click(within(presign).getByRole("button", { name: "Close" }));
  await user.click(screen.getByTitle("Bucket lifecycle"));
  const lifecycle = screen.getByRole("dialog", { name: /lifecycle/i });
  await within(lifecycle).findByText("No rules to display.");
  await user.click(within(lifecycle).getByRole("button", { name: "Close" }));
  await user.click(screen.getByTitle("Create a small object"));
  const upload = screen.getByRole("dialog", { name: "Upload S3 objects" });
  expect(upload).toBeVisible();
  await user.click(within(upload).getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  await user.click(screen.getByTitle("Create a small object"));
  rerender(<S3ConnectorConsoleTemplate {...props} session={{ active: false, startedAt: "s3-session" }} />);
  expect(screen.getByText("No active S3 session")).toBeVisible();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("keeps browsing usable without an advertised transfer runtime or optional endpoint metadata", async () => {
  const { rerender, props } = renderActiveConsole();
  await screen.findByTitle("alpha.txt");
  rerender(
    <S3ConnectorConsoleTemplate
      {...props}
      target={gatewayTargetFixture({
        ref: "s3:2:2",
        connector_kind: "s3",
        transfer_runtime_id: undefined,
        name: undefined,
        target_name: undefined,
        config: {},
      })}
    />,
  );
  await screen.findByTitle("alpha.txt");
  expect(screen.getByTitle("Transfer files and folders")).toBeDisabled();
  expect(screen.getByText(/loaded · bucket/)).toBeVisible();
  expect(screen.getByTitle("Create a small object")).toBeEnabled();
});
