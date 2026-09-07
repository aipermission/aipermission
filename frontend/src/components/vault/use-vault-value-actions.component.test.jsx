import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../lib/api";
import { useVaultValueActions } from "./use-vault-value-actions";

vi.mock("../../lib/api", () => ({ apiPost: vi.fn() }));

function deferred() {
  let resolve;
  const promise = new Promise((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

function ValueHarness({ reloadItems = vi.fn(), setAction = vi.fn() }) {
  const values = useVaultValueActions({ reloadItems, setAction });
  const item = { id: 3, name: "API_KEY", value_version: 2, metadata_revision: 4 };
  return (
    <div>
      <p data-testid="reveal">{`${values.reveal.state}:${values.reveal.value}`}</p>
      <p data-testid="replace">{`${values.replace.preview_state}:${values.replace.preview_value}`}</p>
      <p data-testid="remove">{values.remove.state}</p>
      <button type="button" onClick={() => void values.openReveal(item)}>
        Reveal
      </button>
      <button type="button" onClick={values.closeReveal}>
        Close reveal
      </button>
      <button type="button" onClick={() => values.openReplace(item)}>
        Replace
      </button>
      <button type="button" onClick={() => void values.generateReplacementPreview(item, "hex_secret")}>
        Preview
      </button>
      <button type="button" onClick={() => values.openRemove(item)}>
        Remove
      </button>
      <button type="button" onClick={() => void values.deleteItem()}>
        Delete
      </button>
    </div>
  );
}

beforeEach(() => {
  apiPost.mockReset();
});

it("owns reveal and generated preview reads", async () => {
  const user = userEvent.setup();
  apiPost.mockImplementation((path) =>
    Promise.resolve(path.endsWith("/reveal") ? { value: "secret" } : { value: "preview", preview_token: "token" }),
  );
  render(<ValueHarness />);
  await user.click(screen.getByRole("button", { name: "Reveal" }));
  expect(await screen.findByTestId("reveal")).toHaveTextContent("ready:secret");
  await user.click(screen.getByRole("button", { name: "Replace" }));
  await user.click(screen.getByRole("button", { name: "Preview" }));
  expect(await screen.findByTestId("replace")).toHaveTextContent("ready:preview");
  expect(apiPost).toHaveBeenLastCalledWith(
    "/api/vault-items/3/generate-preview",
    { generator_kind: "hex_secret" },
    { signal: expect.any(AbortSignal) },
  );
});

it("deletes with optimistic revisions and refreshes metadata", async () => {
  const user = userEvent.setup();
  const reloadItems = vi.fn().mockResolvedValue(undefined);
  const setAction = vi.fn();
  apiPost.mockResolvedValue({});
  render(<ValueHarness reloadItems={reloadItems} setAction={setAction} />);
  await user.click(screen.getByRole("button", { name: "Remove" }));
  await user.click(screen.getByRole("button", { name: "Delete" }));
  await waitFor(() => expect(reloadItems).toHaveBeenCalledOnce());
  expect(apiPost).toHaveBeenCalledWith("/api/vault-items/3/delete", { expected_value_version: 2, expected_metadata_revision: 4 });
  expect(setAction).toHaveBeenCalledWith(expect.objectContaining({ message: expect.stringContaining("deleted") }));
});

it("cancels a pending reveal when its dialog closes", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  apiPost.mockReturnValue(pending.promise);
  render(<ValueHarness />);
  await user.click(screen.getByRole("button", { name: "Reveal" }));
  const requestOptions = apiPost.mock.calls[0][2];
  await user.click(screen.getByRole("button", { name: "Close reveal" }));
  expect(requestOptions.signal.aborted).toBe(true);
  pending.resolve({ value: "stale-secret" });
  await waitFor(() => expect(screen.getByTestId("reveal")).toHaveTextContent("idle:"));
});
