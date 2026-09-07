import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiPost, apiPostForm } from "../lib/api";
import { UnlockPage } from "./unlock";

vi.mock("../lib/api", () => ({
  apiPost: vi.fn(),
  apiPostForm: vi.fn(),
}));

const status = {
  database_id: "db-1",
  databases: [{ id: "db-1", name: "Default", state: "locked" }],
};

function deferred() {
  let resolve;
  const promise = new Promise((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

describe("UnlockPage", () => {
  beforeEach(() => {
    apiPost.mockReset();
    apiPostForm.mockReset();
  });

  it("unlocks the selected encrypted database and preserves backend failures", async () => {
    const user = userEvent.setup();
    const onUnlocked = vi.fn();
    apiPost.mockRejectedValueOnce(new Error("Invalid password")).mockResolvedValueOnce({});
    render(<UnlockPage status={status} onUnlocked={onUnlocked} />);

    const password = screen.getByLabelText("Database password");
    await user.click(screen.getByText("Database password", { selector: "label" }));
    expect(password).toHaveFocus();
    await user.type(password, "wrong-password");
    await user.click(screen.getByRole("button", { name: "Unlock", exact: true }));
    expect(await screen.findByText("Invalid password")).toBeVisible();

    await user.clear(password);
    await user.type(password, "CorrectPassword123");
    await user.click(screen.getByRole("button", { name: "Unlock", exact: true }));
    await waitFor(() => expect(onUnlocked).toHaveBeenCalledOnce());
    expect(apiPost).toHaveBeenLastCalledWith(
      "/api/unlock",
      { database_id: "db-1", password: "CorrectPassword123" },
      { signal: expect.any(AbortSignal) },
    );
  });

  it("keeps unlock navigation within narrow viewports", () => {
    render(<UnlockPage status={status} onUnlocked={vi.fn()} />);

    const tabs = screen.getByRole("button", { name: "Unlock Database" }).parentElement;
    expect(tabs).toHaveClass("grid-cols-2", "sm:grid-cols-4");
  });

  it("turns a migration conflict into guidance and requires password plus name before deletion", async () => {
    const user = userEvent.setup();
    const migrationError = Object.assign(new Error("database uses a pre-0.2 schema; use migration helper"), { status: 409 });
    apiPost.mockRejectedValueOnce(migrationError).mockResolvedValueOnce({});
    render(<UnlockPage status={status} onUnlocked={vi.fn()} />);

    await user.type(screen.getByLabelText("Database password"), "OldPassword123");
    await user.click(screen.getByRole("button", { name: "Unlock", exact: true }));
    expect(await screen.findByRole("link", { name: "Open migration helper" })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Delete old local copy" }));
    const confirm = screen.getByLabelText("Type the database name to confirm");
    expect(screen.getByRole("button", { name: "Delete permanently" })).toBeDisabled();
    await user.type(confirm, "Default");
    await user.click(screen.getByRole("button", { name: "Delete permanently" }));
    expect(apiPost).toHaveBeenLastCalledWith(
      "/api/databases/delete-locked",
      {
        database_id: "db-1",
        current_password: "OldPassword123",
      },
      { signal: expect.any(AbortSignal) },
    );
  });

  it("validates creation locally and reports an import without a selected file", async () => {
    const user = userEvent.setup();
    render(<UnlockPage status={{ databases: [] }} onUnlocked={vi.fn()} />);

    const restoreTab = screen.getByRole("button", { name: "Restore Remote" });
    expect(restoreTab.parentElement).toHaveClass("grid-cols-2", "sm:grid-cols-3");
    expect(restoreTab).toHaveClass("col-span-2", "sm:col-span-1");

    const create = screen.getByRole("button", { name: "Create encrypted database" });
    expect(create).toBeDisabled();
    await user.type(screen.getByLabelText("Database password"), "StrongDatabase123");
    await user.type(screen.getByLabelText("Confirm password"), "StrongDatabase123");
    expect(create).toBeEnabled();

    await user.click(screen.getByRole("button", { name: "Import Database" }));
    fireEvent.submit(screen.getByRole("button", { name: "Import database" }).closest("form"));
    expect(await screen.findByText("Database file is required")).toBeVisible();
    expect(apiPostForm).not.toHaveBeenCalled();
  });

  it("owns create failures and successful completion within the create workflow", async () => {
    const user = userEvent.setup();
    const onUnlocked = vi.fn();
    apiPost.mockRejectedValueOnce(new Error("Create failed")).mockResolvedValueOnce({});
    render(<UnlockPage status={{ databases: [] }} onUnlocked={onUnlocked} />);

    await user.type(screen.getByLabelText("Database password"), "StrongDatabase123");
    await user.type(screen.getByLabelText("Confirm password"), "StrongDatabase123");
    await user.click(screen.getByRole("button", { name: "Create encrypted database" }));
    expect(await screen.findByText("Create failed")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Create encrypted database" }));

    await waitFor(() => expect(onUnlocked).toHaveBeenCalledOnce());
    expect(apiPost).toHaveBeenLastCalledWith(
      "/api/unlock/setup",
      { database_name: "", password: "StrongDatabase123", confirm_password: "StrongDatabase123" },
      { signal: expect.any(AbortSignal) },
    );
  });

  it("owns import failures and successful completion within the import workflow", async () => {
    const user = userEvent.setup();
    const onUnlocked = vi.fn();
    apiPostForm.mockRejectedValueOnce(new Error("Import failed")).mockResolvedValueOnce({});
    render(<UnlockPage status={{ databases: [] }} onUnlocked={onUnlocked} />);
    await user.click(screen.getByRole("button", { name: "Import Database" }));
    await user.type(screen.getByLabelText("Database name"), "Imported");
    fireEvent.change(screen.getByLabelText("Database file"), {
      target: { files: [new File(["encrypted"], "backup.aipdb", { type: "application/octet-stream" })] },
    });
    await user.type(screen.getByLabelText("Database password"), "ImportPassword123");
    const submit = screen.getByRole("button", { name: "Import database" });
    fireEvent.submit(submit.closest("form"));
    expect(await screen.findByText("Import failed")).toBeVisible();
    fireEvent.submit(submit.closest("form"));

    await waitFor(() => expect(onUnlocked).toHaveBeenCalledOnce());
    const [requestPath, body, options] = apiPostForm.mock.calls.at(-1);
    expect(requestPath).toBe("/api/backup/import");
    expect(body.get("database_name")).toBe("Imported");
    expect(body.get("database_password")).toBe("ImportPassword123");
    expect(body.get("sqlite")).toBeInstanceOf(File);
    expect(options.signal).toBeInstanceOf(AbortSignal);
  });

  it("cancels an unlock request when the selected database changes", async () => {
    const user = userEvent.setup();
    const pending = deferred();
    const onUnlocked = vi.fn();
    apiPost.mockReturnValueOnce(pending.promise);
    render(
      <UnlockPage
        status={{
          database_id: "db-1",
          databases: [
            { id: "db-1", name: "First", state: "locked" },
            { id: "db-2", name: "Second", state: "locked" },
          ],
        }}
        onUnlocked={onUnlocked}
      />,
    );

    await user.type(screen.getByLabelText("Database password"), "FirstPassword123");
    await user.click(screen.getByRole("button", { name: "Unlock", exact: true }));
    const requestOptions = apiPost.mock.calls[0][2];
    await user.selectOptions(screen.getByLabelText("Database"), "db-2");
    expect(requestOptions.signal.aborted).toBe(true);
    expect(screen.getByLabelText("Database password")).toHaveValue("");

    pending.resolve({});
    await Promise.resolve();
    expect(onUnlocked).not.toHaveBeenCalled();
  });

  it("cancels create and import requests when their workflow unmounts", async () => {
    const user = userEvent.setup();
    const createPending = deferred();
    const importPending = deferred();
    const onUnlocked = vi.fn();
    apiPost.mockReturnValueOnce(createPending.promise);
    apiPostForm.mockReturnValueOnce(importPending.promise);
    const view = render(<UnlockPage status={{ databases: [] }} onUnlocked={onUnlocked} />);

    await user.type(screen.getByLabelText("Database password"), "StrongDatabase123");
    await user.type(screen.getByLabelText("Confirm password"), "StrongDatabase123");
    await user.click(screen.getByRole("button", { name: "Create encrypted database" }));
    const createOptions = apiPost.mock.calls[0][2];
    await user.click(screen.getByRole("button", { name: "Import Database" }));
    expect(createOptions.signal.aborted).toBe(true);

    await user.type(screen.getByLabelText("Database name"), "Imported");
    fireEvent.change(screen.getByLabelText("Database file"), {
      target: { files: [new File(["encrypted"], "backup.aipdb", { type: "application/octet-stream" })] },
    });
    await user.type(screen.getByLabelText("Database password"), "ImportPassword123");
    fireEvent.submit(screen.getByRole("button", { name: "Import database" }).closest("form"));
    await waitFor(() => expect(apiPostForm).toHaveBeenCalledOnce());
    const importOptions = apiPostForm.mock.calls[0][2];
    view.unmount();
    expect(importOptions.signal.aborted).toBe(true);

    createPending.resolve({});
    importPending.resolve({});
    await Promise.resolve();
    expect(onUnlocked).not.toHaveBeenCalled();
  });
});
