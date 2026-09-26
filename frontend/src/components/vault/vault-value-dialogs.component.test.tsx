import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../lib/api";
import { useVaultValueActions } from "./use-vault-value-actions.ts";
import { VaultValueDialogs } from "./vault-value-dialogs.tsx";

vi.mock("../../lib/api", () => ({ apiPost: vi.fn() }));
const post = vi.mocked(apiPost);
function Harness() {
  const owner = useVaultValueActions({ reloadItems: vi.fn(), setAction: vi.fn() });
  const item = { id: 3, name: "DEPLOY_KEY", value_version: 2, metadata_revision: 4 };
  return <>
    <button onClick={() => void owner.openReveal(item)}>Reveal item</button>
    <button onClick={() => owner.openReplace(item)}>Replace item</button>
    <button onClick={() => owner.openRemove(item)}>Remove item</button>
    <VaultValueDialogs owner={owner} />
  </>;
}
beforeEach(() => { post.mockReset(); });

it("shows revealed values only in the local dialog and clears them on close", async () => {
  const user = userEvent.setup();
  post.mockResolvedValue({ value: "local-only-value" });
  render(<Harness />);
  await user.click(screen.getByRole("button", { name: "Reveal item" }));
  expect(await screen.findByDisplayValue("local-only-value")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Copy value" }));
  expect(await screen.findByRole("button", { name: "Copied" })).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Close dialog" }));
  expect(screen.queryByDisplayValue("local-only-value")).not.toBeInTheDocument();
});

it("requires a visible generated preview before saving its token", async () => {
  const user = userEvent.setup();
  post.mockImplementation((path) => Promise.resolve(path.endsWith("/generate-preview")
    ? { value: "candidate-value", preview_token: "candidate-token" } : {}));
  render(<Harness />);
  await user.click(screen.getByRole("button", { name: "Replace item" }));
  expect(screen.getByRole("button", { name: "Replace local value" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Generate locally" }));
  expect(await screen.findByDisplayValue("candidate-value")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Regenerate" }));
  await user.click(screen.getByRole("button", { name: "Save generated value" }));
  await waitFor(() => expect(post).toHaveBeenCalledWith("/api/vault-items/3/value",
    expect.objectContaining({ preview_token: "candidate-token", expected_value_version: 2, value: "" }), expect.any(Object)));
});

it("requires the exact item name before permanent deletion", async () => {
  const user = userEvent.setup();
  post.mockResolvedValue({});
  render(<Harness />);
  await user.click(screen.getByRole("button", { name: "Remove item" }));
  expect(screen.getByRole("button", { name: "Delete Vault item" })).toBeDisabled();
  await user.type(screen.getByRole("textbox"), "DEPLOY_KEY");
  await user.click(screen.getByRole("button", { name: "Delete Vault item" }));
  expect(post).toHaveBeenCalledWith("/api/vault-items/3/delete",
    { expected_value_version: 2, expected_metadata_revision: 4 }, expect.any(Object));
});
