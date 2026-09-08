import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { ProvisionUserDialog } from "./provision-user-dialog";
import { usePostgresProvisioning } from "./use-postgres-provisioning";

vi.mock("./use-postgres-provisioning", () => ({ usePostgresProvisioning: vi.fn() }));

beforeEach(() => {
  usePostgresProvisioning.mockReturnValue({
    state: { state: "ready", error: "Credential refresh warning", result: { result: {}, profile: { label: "reader" } } },
    metadata: { state: "pending", error: "Approval required", schemas: [] },
    form: { role_name: "reader", profile_label: "", preset: "read_only" },
    scope: { all_schemas: true },
    scopeSummary: "All visible schemas",
    sqlPreview: "CREATE ROLE reader;",
    targetRef: "postgres:1:1",
    canSubmit: true,
    updateForm: vi.fn(),
    setScope: vi.fn((update) => update({ all_schemas: true })),
    loadMetadata: vi.fn(),
    provisionUser: vi.fn((event) => event.preventDefault()),
  });
});

it("renders managed user feedback and delegates form actions", async () => {
  const user = userEvent.setup();
  const onClose = vi.fn();
  render(<ProvisionUserDialog value={{ open: true, target: { name: "Main DB" } }} onClose={onClose} onOperationComplete={vi.fn()} />);
  expect(screen.getByText("All visible schemas")).toBeVisible();
  expect(screen.getByText("Approval required")).toBeVisible();
  expect(screen.getByText(/New profile:/)).toBeVisible();
  await user.clear(screen.getByLabelText("Role name"));
  await user.type(screen.getByLabelText("Role name"), "writer");
  await user.type(screen.getByLabelText("Profile label"), "Writer");
  await user.selectOptions(screen.getByLabelText("Preset"), "read_write");
  await user.click(screen.getByRole("checkbox", { name: "Select all schemas, tables, and columns" }));
  await user.click(screen.getByRole("button", { name: "Refresh" }));
  await user.click(screen.getByRole("button", { name: "Close" }));
  expect(usePostgresProvisioning.mock.results[0].value.loadMetadata).toHaveBeenCalledOnce();
  expect(onClose).toHaveBeenCalledOnce();
});
