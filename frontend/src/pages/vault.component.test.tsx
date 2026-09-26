import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet as realGet, apiPost as realPost } from "../lib/api";
import { VaultPage } from "./vault";
import type { VaultManagedItem } from "../lib/gateway-contracts/vault-management-contract";

vi.mock("../lib/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../lib/api")>()),
  apiGet: vi.fn(),
  apiPost: vi.fn(),
}));
const apiGet = vi.mocked(realGet);
const apiPost = vi.mocked(realPost);
const item: VaultManagedItem = {
  id: 9,
  name: "PROJECT_API_KEY",
  owner_project_id: 4,
  owner_project_name: "My Project",
  project_ids: [4],
  source: "imported",
  secret_type: "generic_secret",
  value_version: 1,
  metadata_revision: 1,
  tags: [],
  usage_notes: [],
};

beforeEach(() => {
  apiGet.mockReset();
  apiPost.mockReset();
  apiGet.mockImplementation(async (path) => {
    if (path === "/api/projects") return { items: [{ id: 4, name: "My Project", slug: "my-project", target_count: 0 }] };
    if (path.startsWith("/api/vault-items")) return { items: [item], total: 20 };
    if (path.startsWith("/api/vault-default-bindings") || path === "/api/connector-targets/inventory") return { items: [] };
    throw new Error(`Unexpected Vault page read ${path}`);
  });
  apiPost.mockResolvedValue({ value: "local-only-preview" });
});

it("lists metadata, reports bounded results and requires an explicit reveal action", async () => {
  const user = userEvent.setup();
  render(<VaultPage />);
  expect(await screen.findByText("PROJECT_API_KEY")).toBeVisible();
  expect(screen.getByText(/Showing 1 of 20 matching Vault items/)).toBeVisible();
  expect(apiPost).not.toHaveBeenCalled();
  expect(screen.queryByText("local-only-preview")).not.toBeInTheDocument();
  const reads = apiGet.mock.calls.length;
  await user.click(screen.getByRole("button", { name: "Refresh" }));
  await waitFor(() => expect(apiGet.mock.calls.length).toBeGreaterThan(reads));
  await user.click(screen.getByTitle("Reveal and copy"));
  expect(await screen.findByRole("dialog", { name: "Reveal PROJECT_API_KEY" })).toBeVisible();
  await waitFor(() => expect(apiPost).toHaveBeenCalledWith("/api/vault-items/9/reveal", {}, { signal: expect.any(AbortSignal) }));
  expect(await screen.findByDisplayValue("local-only-preview")).toBeVisible();
});

it("opens the collection editor and closes it without sending a mutation", async () => {
  const user = userEvent.setup();
  render(<VaultPage />);
  await screen.findByText("PROJECT_API_KEY");
  await user.click(screen.getByRole("button", { name: "Add vault item" }));
  expect(screen.getByRole("textbox", { name: /Environment name/ })).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("textbox", { name: /Environment name/ })).not.toBeInTheDocument();
  expect(apiPost).not.toHaveBeenCalled();
});

it("loads default bindings only after the operator opens their dialog", async () => {
  const user = userEvent.setup();
  render(<VaultPage />);
  await screen.findByText("PROJECT_API_KEY");
  expect(apiGet.mock.calls.some(([path]) => path.startsWith("/api/vault-default-bindings"))).toBe(false);
  await user.click(screen.getByTitle("Default session bindings"));
  expect(await screen.findByRole("dialog", { name: "Default environment for PROJECT_API_KEY" })).toBeVisible();
  await waitFor(() =>
    expect(apiGet).toHaveBeenCalledWith("/api/vault-default-bindings?vault_item_id=9", { signal: expect.any(AbortSignal) }),
  );
  expect(apiPost).not.toHaveBeenCalled();
});

it("keeps failed metadata reads visible and creation unavailable without projects", async () => {
  apiGet.mockImplementation(async (path) => {
    throw new Error(path === "/api/projects" ? "Projects unavailable" : "Vault metadata unavailable");
  });
  render(<VaultPage />);
  expect(await screen.findByText("Projects unavailable")).toBeVisible();
  expect(await screen.findByText("Vault metadata unavailable")).toBeVisible();
  expect(screen.getByRole("button", { name: "Add vault item" })).toBeDisabled();
  expect(apiPost).not.toHaveBeenCalled();
});
