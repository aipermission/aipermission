import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { renderCredentialFamily } from "../../../test/render-credential-family";
import { captureCredentialFamily } from "../../editor/capture-credential-family";
import { sshCredentialFamily } from "./credential-family";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn() }));
const familyTemplate = sshCredentialFamily.create(captureCredentialFamily);
beforeEach(() => vi.mocked(apiPost).mockReset().mockResolvedValue({}));

async function openImport() {
  const user = userEvent.setup();
  const family = renderCredentialFamily(familyTemplate);
  act(() => family.openCreate());
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Import" }));
  return { ...family, user };
}

function readFile(name: string) {
  let resolve!: (_text: string) => void;
  let reject!: (_error: Error) => void;
  const file = new File(["fixture-only"], name);
  const text = vi.fn(
    () =>
      new Promise<string>((done, fail) => {
        resolve = done;
        reject = fail;
      }),
  );
  Object.defineProperty(file, "text", { value: text });
  const input = screen.getByRole("dialog").querySelector<HTMLInputElement>('input[type="file"]');
  if (!input) throw new Error("Missing native key-file input");
  fireEvent.change(input, { target: { files: [file] } });
  expect(text).toHaveBeenCalledOnce();
  return { resolve, reject };
}

it("imports the newest file when an older read completes last", async () => {
  const { user } = await openImport();
  const first = readFile("first.pem");
  const latest = readFile("latest.pem");
  await act(async () => latest.resolve("latest-fixture-key"));
  await act(async () => first.resolve("retired-fixture-key"));
  expect(screen.getByRole("textbox", { name: "Private key" })).toHaveValue("latest-fixture-key");
  expect(screen.getByRole("textbox", { name: "Name" })).toHaveValue("latest");
  await user.click(screen.getByRole("button", { name: "Import credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(apiPost).toHaveBeenCalledWith("/api/connectors/ssh/credentials/import", {
    name: "latest",
    private_key: "latest-fixture-key",
    passphrase: "",
  });
});

it("retires file reads across Generate then Import without restoring cleared secrets", async () => {
  const { user } = await openImport();
  const first = readFile("first.pem");
  await user.click(screen.getByRole("button", { name: "Generate" }));
  await user.click(screen.getByRole("button", { name: "Import" }));
  await act(async () => first.resolve("retired-fixture-key"));
  expect(screen.getByRole("textbox", { name: "Private key" })).toHaveValue("");
  expect(screen.getByRole("textbox", { name: "Name" })).toHaveValue("imported-key");
  expect(apiPost).not.toHaveBeenCalled();
});

it("preserves manual key replacement while a file read is pending", async () => {
  await openImport();
  const first = readFile("first.pem");
  fireEvent.change(screen.getByRole("textbox", { name: "Private key" }), { target: { value: "manual-fixture-key" } });
  await act(async () => first.resolve("retired-fixture-key"));
  expect(screen.getByRole("textbox", { name: "Private key" })).toHaveValue("manual-fixture-key");
});

it("keeps a pending replacement when the already-active Import tab is clicked", async () => {
  const { user } = await openImport();
  fireEvent.change(screen.getByRole("textbox", { name: "Private key" }), { target: { value: "previous-fixture-key" } });
  const file = readFile("replacement.pem");
  await user.click(screen.getByRole("button", { name: "Import" }));
  expect(screen.getByRole("button", { name: "Reading key file..." })).toBeDisabled();
  await act(async () => file.resolve("replacement-fixture-key"));
  await user.click(screen.getByRole("button", { name: "Import credential" }));
  await waitFor(() =>
    expect(apiPost).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ private_key: "replacement-fixture-key" })),
  );
});

it("shows the current import failure after a failed replacement read", async () => {
  const { user } = await openImport();
  fireEvent.change(screen.getByRole("textbox", { name: "Private key" }), { target: { value: "previous-fixture-key" } });
  const file = readFile("replacement.pem");
  await act(async () => file.reject(new Error("unreadable-file")));
  vi.mocked(apiPost).mockRejectedValueOnce(new Error("Import action failed."));
  await user.click(screen.getByRole("button", { name: "Import credential" }));
  await waitFor(() => expect(screen.getByText("Import action failed.")).toBeVisible());
  expect(screen.queryByText("The key file could not be read. Choose it again or paste the key.")).not.toBeInTheDocument();
});

it("allows label and passphrase edits without replacing them on file completion", async () => {
  await openImport();
  const file = readFile("first.pem");
  fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "custom-label" } });
  fireEvent.change(screen.getByLabelText("Passphrase"), { target: { value: "fixture-passphrase" } });
  await act(async () => file.resolve("first-fixture-key"));
  expect(screen.getByRole("textbox", { name: "Name" })).toHaveValue("custom-label");
  expect(screen.getByLabelText("Passphrase")).toHaveValue("fixture-passphrase");
  expect(screen.getByRole("textbox", { name: "Private key" })).toHaveValue("first-fixture-key");
});

it("cannot fill a replacement dialog after close and reopen", async () => {
  const family = await openImport();
  const file = readFile("first.pem");
  act(() => family.close());
  act(() => family.openCreate());
  await family.user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Import" }));
  await act(async () => file.resolve("retired-fixture-key"));
  expect(screen.getByRole("textbox", { name: "Private key" })).toHaveValue("");
});

it("does not submit the previous key while a replacement file is still reading", async () => {
  await openImport();
  fireEvent.change(screen.getByRole("textbox", { name: "Private key" }), { target: { value: "previous-fixture-key" } });
  const file = readFile("latest.pem");
  const submit = screen.getByRole("button", { name: "Reading key file..." });
  expect(submit).toBeDisabled();
  const form = submit.closest("form");
  if (!form) throw new Error("Missing import form");
  fireEvent.submit(form);
  expect(apiPost).not.toHaveBeenCalled();
  await act(async () => file.resolve("latest-fixture-key"));
  expect(screen.getByRole("button", { name: "Import credential" })).toBeEnabled();
});

it("contains file-read failures, permits retry, and never displays the raw failure", async () => {
  await openImport();
  const first = readFile("first.pem");
  await act(async () => first.reject(new Error("fixture-private-content-not-for-display")));
  expect(screen.getByText("The key file could not be read. Choose it again or paste the key.")).toBeVisible();
  expect(screen.queryByText("fixture-private-content-not-for-display")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Import credential" })).toBeEnabled();
  const retry = readFile("retry.pem");
  await act(async () => retry.resolve("retry-fixture-key"));
  expect(screen.queryByText("The key file could not be read. Choose it again or paste the key.")).not.toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "Private key" })).toHaveValue("retry-fixture-key");
});

it("ignores errors from retired reads while the current read stays pending", async () => {
  await openImport();
  const first = readFile("first.pem");
  const latest = readFile("latest.pem");
  await act(async () => first.reject(new Error("retired-read")));
  expect(screen.getByRole("button", { name: "Reading key file..." })).toBeDisabled();
  expect(screen.queryByText("The key file could not be read. Choose it again or paste the key.")).not.toBeInTheDocument();
  await act(async () => latest.resolve("latest-fixture-key"));
  expect(screen.getByRole("textbox", { name: "Private key" })).toHaveValue("latest-fixture-key");
});

it("treats a canceled file picker as no replacement and retires reads on unmount", async () => {
  const family = await openImport();
  const file = readFile("first.pem");
  const input = screen.getByRole("dialog").querySelector<HTMLInputElement>('input[type="file"]');
  if (!input) throw new Error("Missing file input");
  fireEvent.change(input, { target: { files: [] } });
  expect(screen.getByRole("button", { name: "Reading key file..." })).toBeDisabled();
  family.unmount();
  await act(async () => file.resolve("retired-fixture-key"));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(apiPost).not.toHaveBeenCalled();
});
