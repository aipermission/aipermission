import { render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet } from "../lib/api";
import { useGateway } from "../lib/gateway-context";
import { useCredentialProfileEditor } from "../connectors/editor/use-credential-profile-editor";
import { emptyCredentialState } from "../connectors/templates/ssh/model";
import { CredentialsPage } from "./credentials";

vi.mock("../lib/api", () => ({ apiGet: vi.fn() }));
vi.mock("../lib/gateway-context", () => ({ useGateway: vi.fn() }));
vi.mock("../connectors/editor/use-credential-profile-editor", () => ({ useCredentialProfileEditor: vi.fn() }));

const editorFixture: ReturnType<typeof useCredentialProfileEditor> = {
  drawer: { open: false, mode: "create", kind: "ssh", row: null },
  formState: {},
  setFormState: vi.fn(),
  actionState: { state: "idle", error: "", message: "" },
  openCreate: vi.fn(),
  openEdit: vi.fn().mockReturnValue(true),
  closeEditor: vi.fn(),
  save: vi.fn().mockResolvedValue(true),
  remove: vi.fn().mockResolvedValue(true),
};

beforeEach(() => {
  vi.mocked(apiGet).mockResolvedValue({ items: [] });
  vi.mocked(useGateway, { partial: true }).mockReturnValue({
    credentials: { state: "ready", data: [], errors: [], error: null },
    loadCredentials: vi.fn().mockResolvedValue([]),
  });
  vi.mocked(useCredentialProfileEditor).mockReturnValue(editorFixture);
});

it("loads the generic credential inventories and shows the empty state", async () => {
  render(<CredentialsPage />);
  expect(await screen.findByText("Create your first connector credential.")).toBeVisible();
  expect(apiGet).toHaveBeenCalledWith("/api/connectors");
  expect(apiGet).toHaveBeenCalledWith("/api/connector-targets/inventory");
});

it("disables credential fields and mode changes while a save is pending", () => {
  vi.mocked(useCredentialProfileEditor).mockReturnValue({
    ...editorFixture,
    drawer: { open: true, mode: "create", kind: "ssh", row: null },
    formState: emptyCredentialState(),
    actionState: { state: "saving", error: null, message: null },
  });

  render(<CredentialsPage />);

  expect(screen.getByRole("textbox", { name: "Name" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Import" })).toBeDisabled();
  expect(screen.getByRole("button", { name: /Generating|Creating|Generate .* credential/ })).toBeDisabled();
});
