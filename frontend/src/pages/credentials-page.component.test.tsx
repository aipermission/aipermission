import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../lib/api";
import { useGateway } from "../lib/gateway-context";
import { CredentialsPage } from "./credentials";

vi.mock("../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn() }));
vi.mock("../lib/gateway-context", () => ({ useGateway: vi.fn() }));

beforeEach(() => {
  vi.mocked(apiGet).mockResolvedValue({ items: [] });
  vi.mocked(useGateway, { partial: true }).mockReturnValue({
    credentials: { state: "ready", data: [], errors: [], error: null },
    loadCredentials: vi.fn().mockResolvedValue([]),
  });
  vi.mocked(apiPost).mockReset().mockResolvedValue({});
});

it("loads the generic credential inventories and shows the empty state", async () => {
  render(<CredentialsPage />);
  expect(await screen.findByText("Create your first connector credential.")).toBeVisible();
  expect(apiGet).toHaveBeenCalledWith("/api/connectors", { signal: expect.any(AbortSignal) });
  expect(apiGet).toHaveBeenCalledWith("/api/connector-targets/inventory", { signal: expect.any(AbortSignal) });
});

it("disables credential fields and mode changes while a save is pending", async () => {
  const user = userEvent.setup();
  vi.mocked(apiGet).mockImplementation(async (path) => ({
    items: path === "/api/connectors" ? [{ kind: "ssh", label: "SSH", version: "0.2" }] : [],
  }));
  let finish: (_value: object) => void = () => {
    throw new Error("Native save is not pending");
  };
  vi.mocked(apiPost).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  render(<CredentialsPage />);
  await screen.findByText("Create your first connector credential.");
  await user.click(screen.getByRole("button", { name: "Add credential" }));
  await user.click(screen.getByRole("menuitem", { name: /SSH/ }));
  await user.type(screen.getByRole("textbox", { name: "Name" }), "Test key");
  await user.click(screen.getByRole("button", { name: /Generate .* credential/ }));
  expect(screen.getByRole("textbox", { name: "Name" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Import" })).toBeDisabled();
  expect(screen.getByRole("button", { name: /Generating|Creating|Generate .* credential/ })).toBeDisabled();
  expect(apiPost).toHaveBeenCalledOnce();
  await act(async () => finish({}));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
});
