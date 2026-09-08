import { render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet } from "../lib/api";
import { useGateway } from "../lib/gateway-context";
import { useCredentialProfileEditor } from "../connectors/editor/use-credential-profile-editor";
import { CredentialsPage } from "./credentials";

vi.mock("../lib/api", () => ({ apiGet: vi.fn() }));
vi.mock("../lib/gateway-context", () => ({ useGateway: vi.fn() }));
vi.mock("../connectors/editor/use-credential-profile-editor", () => ({ useCredentialProfileEditor: vi.fn() }));

beforeEach(() => {
  apiGet.mockImplementation(async (path) => (path === "/api/connectors" ? { items: [] } : { items: [] }));
  useGateway.mockReturnValue({ credentials: { state: "ready", data: [], errors: [] }, loadCredentials: vi.fn() });
  useCredentialProfileEditor.mockReturnValue({
    drawer: { open: false, mode: "create", kind: "ssh" },
    formState: {},
    setFormState: vi.fn(),
    actionState: { state: "idle", error: "", message: "" },
    openCreate: vi.fn(),
    openEdit: vi.fn(),
    closeEditor: vi.fn(),
    save: vi.fn(),
    remove: vi.fn(),
  });
});

it("loads the generic credential inventories and shows the empty state", async () => {
  render(<CredentialsPage />);
  expect(await screen.findByText("Create your first connector credential.")).toBeVisible();
  expect(apiGet).toHaveBeenCalledWith("/api/connectors");
  expect(apiGet).toHaveBeenCalledWith("/api/connector-targets/inventory");
});
