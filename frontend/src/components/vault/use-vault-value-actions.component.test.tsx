import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost as realPost } from "../../lib/api";
import { useVaultValueActions } from "./use-vault-value-actions";

vi.mock("../../lib/api", () => ({ apiPost: vi.fn() }));
const apiPost = vi.mocked(realPost);

function requestSignal() {
  const options = apiPost.mock.calls[0]?.[2];
  const signal = options && "signal" in options ? options.signal : undefined;
  if (!(signal instanceof AbortSignal)) throw new Error("Missing mutation signal");
  return signal;
}

function deferred() {
  let resolve!: (_value?: unknown) => void;
  let reject!: (_reason?: unknown) => void;
  const promise = new Promise<unknown>((next, failure) => {
    resolve = next;
    reject = failure;
  });
  return { promise, resolve, reject };
}

function ValueHarness({ reloadItems = vi.fn(), setAction = vi.fn() }: Partial<Parameters<typeof useVaultValueActions>[0]>) {
  const values = useVaultValueActions({ reloadItems, setAction });
  const item = { id: 3, name: "API_KEY", value_version: 2, metadata_revision: 4 };
  return (
    <div>
      <p data-testid="reveal">{`${values.reveal.state}:${values.reveal.value}`}</p>
      <p data-testid="reveal-feedback">{`${values.reveal.copied}:${values.reveal.error || ""}`}</p>
      <p data-testid="replace">{`${values.replace.preview_state}:${values.replace.preview_value}`}</p>
      <p data-testid="remove">{values.remove.state}</p>
      <button type="button" onClick={() => void values.openReveal(item)}>
        Reveal
      </button>
      <button type="button" onClick={values.closeReveal}>
        Close reveal
      </button>
      <button type="button" onClick={() => void values.copyRevealedValue()}>
        Copy reveal
      </button>
      <button type="button" onClick={() => values.openReplace(item)}>
        Replace
      </button>
      <button type="button" onClick={values.closeReplace}>
        Close replace
      </button>
      <button type="button" onClick={() => void values.generateReplacementPreview(item, "hex_secret")}>
        Preview
      </button>
      <button type="button" onClick={(event) => void values.replaceValue(event)}>
        Save replacement
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

it("rejects a malformed reveal value without leaving a secret in dialog state", async () => {
  const user = userEvent.setup();
  apiPost.mockResolvedValue({ value: { hidden: "not-a-string" } });
  render(<ValueHarness />);
  await user.click(screen.getByRole("button", { name: "Reveal" }));
  await waitFor(() => expect(screen.getByTestId("reveal")).toHaveTextContent("error:"));
  expect(screen.getByTestId("reveal-feedback")).toHaveTextContent("Invalid Vault value response");
});

it("does not enable saving a generated preview with a malformed approval token", async () => {
  const user = userEvent.setup();
  apiPost.mockResolvedValue({ value: "candidate", preview_token: 4 });
  render(<ValueHarness />);
  await user.click(screen.getByRole("button", { name: "Replace" }));
  await user.click(screen.getByRole("button", { name: "Preview" }));
  await waitFor(() => expect(screen.getByTestId("replace")).toHaveTextContent("error:"));
  expect(screen.getByTestId("replace")).not.toHaveTextContent("candidate");
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
  expect(apiPost).toHaveBeenCalledWith(
    "/api/vault-items/3/delete",
    { expected_value_version: 2, expected_metadata_revision: 4 },
    { signal: expect.any(AbortSignal) },
  );
  expect(setAction).toHaveBeenCalledWith(expect.objectContaining({ message: expect.stringContaining("deleted") }));
});

it("does not let a closed deletion mutate a newly opened dialog", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  const reloadItems = vi.fn();
  const setAction = vi.fn();
  apiPost.mockReturnValue(pending.promise);
  render(<ValueHarness reloadItems={reloadItems} setAction={setAction} />);

  await user.click(screen.getByRole("button", { name: "Remove" }));
  await user.click(screen.getByRole("button", { name: "Delete" }));
  const signal = requestSignal();
  await user.click(screen.getByRole("button", { name: "Remove" }));
  expect(signal.aborted).toBe(true);
  pending.resolve({});

  await waitFor(() => expect(screen.getByTestId("remove")).toHaveTextContent("idle"));
  expect(reloadItems).not.toHaveBeenCalled();
  expect(setAction).not.toHaveBeenCalled();
});

it("cancels a pending reveal when its dialog closes", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  apiPost.mockReturnValue(pending.promise);
  render(<ValueHarness />);
  await user.click(screen.getByRole("button", { name: "Reveal" }));
  const signal = requestSignal();
  await user.click(screen.getByRole("button", { name: "Close reveal" }));
  expect(signal.aborted).toBe(true);
  pending.resolve({ value: "stale-secret" });
  await waitFor(() => expect(screen.getByTestId("reveal")).toHaveTextContent("idle:"));
});

it.each(["resolve", "reject"])("ignores a stale clipboard %s after the reveal dialog is reopened", async (outcome) => {
  const user = userEvent.setup();
  const clipboard = deferred();
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText: vi.fn().mockReturnValue(clipboard.promise) },
  });
  apiPost.mockResolvedValue({ value: "secret" });
  render(<ValueHarness />);

  await user.click(screen.getByRole("button", { name: "Reveal" }));
  await screen.findByText("ready:secret");
  await user.click(screen.getByRole("button", { name: "Copy reveal" }));
  await user.click(screen.getByRole("button", { name: "Close reveal" }));
  await user.click(screen.getByRole("button", { name: "Reveal" }));
  await screen.findByText("ready:secret");

  await act(async () => {
    if (outcome === "resolve") clipboard.resolve();
    else clipboard.reject(new Error("denied"));
    await clipboard.promise.catch(() => undefined);
  });

  expect(navigator.clipboard.writeText).toHaveBeenCalledOnce();
  expect(screen.getByTestId("reveal-feedback")).toHaveTextContent("false:");
});

it("does not let a closed replacement save mutate a reopened dialog", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  const reloadItems = vi.fn();
  const setAction = vi.fn();
  apiPost.mockReturnValue(pending.promise);
  render(<ValueHarness reloadItems={reloadItems} setAction={setAction} />);

  await user.click(screen.getByRole("button", { name: "Replace" }));
  await user.click(screen.getByRole("button", { name: "Save replacement" }));
  const signal = requestSignal();
  await user.click(screen.getByRole("button", { name: "Close replace" }));
  await user.click(screen.getByRole("button", { name: "Replace" }));
  expect(signal.aborted).toBe(true);
  pending.resolve({});

  await waitFor(() => expect(screen.getByTestId("replace")).toHaveTextContent("idle:"));
  expect(reloadItems).not.toHaveBeenCalled();
  expect(setAction).not.toHaveBeenCalled();
});

it("does not restore a generated secret preview after opening a new replacement dialog", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  apiPost.mockReturnValue(pending.promise);
  render(<ValueHarness />);
  await user.click(screen.getByRole("button", { name: "Replace" }));
  await user.click(screen.getByRole("button", { name: "Preview" }));
  const signal = requestSignal();
  await user.click(screen.getByRole("button", { name: "Replace" }));
  expect(signal.aborted).toBe(true);
  await act(async () => pending.resolve({ value: "stale-preview", preview_token: "stale-token" }));
  expect(screen.getByTestId("replace")).toHaveTextContent("idle:");
  expect(screen.getByTestId("replace")).not.toHaveTextContent("stale-preview");
});

it("does not start a preview while a replacement is saving", async () => {
  const user = userEvent.setup();
  const save = deferred();
  apiPost.mockReturnValue(save.promise);
  render(<ValueHarness />);
  await user.click(screen.getByRole("button", { name: "Replace" }));
  await user.click(screen.getByRole("button", { name: "Save replacement" }));
  await user.click(screen.getByRole("button", { name: "Preview" }));
  expect(apiPost).toHaveBeenCalledOnce();
  await act(async () => save.resolve({}));
  expect(screen.getByTestId("replace")).toHaveTextContent("idle:");
});

it("discards an in-flight preview when replacement begins", async () => {
  const user = userEvent.setup();
  const preview = deferred();
  apiPost.mockImplementation((path) => path.endsWith("/generate-preview") ? preview.promise : Promise.resolve({}));
  render(<ValueHarness />);
  await user.click(screen.getByRole("button", { name: "Replace" }));
  await user.click(screen.getByRole("button", { name: "Preview" }));
  const signal = requestSignal();
  await user.click(screen.getByRole("button", { name: "Save replacement" }));
  expect(signal.aborted).toBe(true);
  await act(async () => preview.resolve({ value: "late-secret", preview_token: "late-token" }));
  expect(screen.getByTestId("replace")).toHaveTextContent("idle:");
  expect(screen.getByTestId("replace")).not.toHaveTextContent("late-secret");
});
