import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiPost, apiPostForm } from "../lib/api";
import { UnlockPage } from "./unlock";

// async-owner: src/pages/use-unlock-lifecycle-mutation.js

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

function resetMocks() {
  vi.useRealTimers();
  apiPost.mockReset();
  apiPostForm.mockReset();
}

describe("UnlockPage workflows", () => {
  beforeEach(resetMocks);

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

  it("renders session and unsupported-database guidance", async () => {
    const user = userEvent.setup();
    render(
      <UnlockPage
        status={{
          state: "session_required",
          databases: [{ id: "plain", name: "Plain", state: "unsupported_plaintext" }],
        }}
        onUnlocked={vi.fn()}
      />,
    );

    expect(screen.getByText(/browser session is missing or expired/i)).toBeVisible();
    expect(screen.getByText(/plaintext SQLite database/i)).toBeVisible();
    expect(screen.getByRole("option", { name: /unsupported plaintext/i })).toBeVisible();

    await user.click(screen.getByRole("button", { name: "Restore Remote" }));
    expect(screen.getByRole("button", { name: /connect and list backups/i })).toBeVisible();
  });

  it("reconciles selection when the available database set changes", async () => {
    const view = render(<UnlockPage status={status} onUnlocked={vi.fn()} />);

    view.rerender(<UnlockPage status={{ databases: [] }} onUnlocked={vi.fn()} />);
    expect(await screen.findByRole("button", { name: "Create encrypted database" })).toBeVisible();

    view.rerender(<UnlockPage status={{ databases: [{ id: "db-2", name: "Second", state: "locked" }] }} onUnlocked={vi.fn()} />);
    expect(await screen.findByLabelText("Database")).toHaveValue("db-2");
  });

  it("turns a migration conflict into guidance and requires password plus name before deletion", async () => {
    const user = userEvent.setup();
    const onUnlocked = vi.fn();
    const migrationError = Object.assign(new Error("database uses a pre-0.2 schema; use migration helper"), { status: 409 });
    apiPost.mockRejectedValueOnce(migrationError).mockResolvedValueOnce({});
    render(<UnlockPage status={status} onUnlocked={onUnlocked} />);

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
    expect(await screen.findByRole("status")).toHaveTextContent("Local database deleted.");
    expect(onUnlocked).toHaveBeenCalledOnce();
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
});

describe("UnlockPage lifecycle ownership", () => {
  beforeEach(resetMocks);

  it("locks database selection while an unlock mutation is pending", async () => {
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
    expect(screen.getByLabelText("Database")).toBeDisabled();
    expect(screen.getByRole("button", { name: "New Database" })).toBeDisabled();
    expect(requestOptions.signal.aborted).toBe(false);

    pending.resolve({});
    await waitFor(() => expect(onUnlocked).toHaveBeenCalledOnce());
  });

  it("switches the selected database when no lifecycle mutation is running", async () => {
    const user = userEvent.setup();
    render(
      <UnlockPage
        status={{
          database_id: "db-1",
          databases: [
            { id: "db-1", name: "First", state: "locked" },
            { id: "db-2", name: "Second", state: "locked" },
          ],
        }}
        onUnlocked={vi.fn()}
      />,
    );

    await user.selectOptions(screen.getByLabelText("Database"), "db-2");

    expect(screen.getByLabelText("Database")).toHaveValue("db-2");
  });

  it("validates, cancels, and reports failures from the split delete action", async () => {
    const user = userEvent.setup();
    apiPost.mockRejectedValueOnce(new Error("Delete failed"));
    render(<UnlockPage status={status} onUnlocked={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Choose database action" }));
    await user.click(screen.getByRole("button", { name: "Delete this local database" }));
    await user.click(screen.getByRole("button", { name: "Delete this local database" }));
    expect(await screen.findByText("Enter the database password before deleting this local database.")).toBeVisible();

    await user.type(screen.getByLabelText("Database password"), "DeletePassword123");
    await user.click(screen.getByRole("button", { name: "Delete this local database" }));
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.getByRole("button", { name: "Unlock", exact: true })).toBeVisible();

    await user.click(screen.getByRole("button", { name: "Choose database action" }));
    await user.click(screen.getByRole("button", { name: "Delete this local database" }));
    await user.click(screen.getByRole("button", { name: "Delete this local database" }));
    await user.type(screen.getByLabelText("Type the database name to confirm"), "Default");
    await user.click(screen.getByRole("button", { name: "Delete permanently" }));

    expect(await screen.findByText("Delete failed")).toBeVisible();
  });

  it("removes the deletion toast after its display interval", async () => {
    vi.useFakeTimers();
    apiPost.mockResolvedValueOnce({});
    const onUnlocked = vi.fn();
    render(<UnlockPage status={status} onUnlocked={onUnlocked} />);

    fireEvent.change(screen.getByLabelText("Database password"), { target: { value: "DeletePassword123" } });
    fireEvent.click(screen.getByRole("button", { name: "Choose database action" }));
    fireEvent.click(screen.getByRole("button", { name: "Delete this local database" }));
    fireEvent.click(screen.getByRole("button", { name: "Delete this local database" }));
    fireEvent.change(screen.getByLabelText("Type the database name to confirm"), { target: { value: "Default" } });
    fireEvent.click(screen.getByRole("button", { name: "Delete permanently" }));
    await act(async () => Promise.resolve());

    expect(onUnlocked).toHaveBeenCalledOnce();
    expect(screen.getByRole("status")).toHaveTextContent("Local database deleted.");
    act(() => vi.advanceTimersByTime(2400));
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    vi.useRealTimers();
  });

  it("keeps a lifecycle workflow mounted until its status reconciliation completes", async () => {
    const user = userEvent.setup();
    const createPending = deferred();
    const onUnlocked = vi.fn();
    apiPost.mockReturnValueOnce(createPending.promise);
    const view = render(<UnlockPage status={{ databases: [] }} onUnlocked={onUnlocked} />);

    await user.type(screen.getByLabelText("Database password"), "StrongDatabase123");
    await user.type(screen.getByLabelText("Confirm password"), "StrongDatabase123");
    await user.click(screen.getByRole("button", { name: "Create encrypted database" }));
    const createOptions = apiPost.mock.calls[0][2];
    view.rerender(
      <UnlockPage
        status={{ database_id: "created-db", databases: [{ id: "created-db", name: "Created", state: "locked" }] }}
        onUnlocked={onUnlocked}
      />,
    );
    expect(screen.getByRole("button", { name: "Working..." })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Unlock", exact: true })).not.toBeInTheDocument();
    expect(createOptions.signal.aborted).toBe(false);

    createPending.resolve({});
    await waitFor(() => expect(onUnlocked).toHaveBeenCalledOnce());
    await waitFor(() => expect(screen.getByRole("button", { name: "Unlock", exact: true })).toBeVisible());
  });

  it("aborts lifecycle reconciliation when the unlock page unmounts", async () => {
    const user = userEvent.setup();
    const createPending = deferred();
    const onUnlocked = vi.fn();
    apiPost.mockReturnValueOnce(createPending.promise);
    const view = render(<UnlockPage status={{ databases: [] }} onUnlocked={onUnlocked} />);

    await user.type(screen.getByLabelText("Database password"), "StrongDatabase123");
    await user.type(screen.getByLabelText("Confirm password"), "StrongDatabase123");
    await user.click(screen.getByRole("button", { name: "Create encrypted database" }));
    const requestOptions = apiPost.mock.calls[0][2];
    view.unmount();

    expect(requestOptions.signal.aborted).toBe(true);
    createPending.resolve({});
    await Promise.resolve();
    expect(onUnlocked).not.toHaveBeenCalled();
  });

  it("aborts status reconciliation after a completed lifecycle mutation unmounts", async () => {
    const user = userEvent.setup();
    const reconciliation = deferred();
    let reconciliationSignal;
    apiPost.mockResolvedValueOnce({});
    const onUnlocked = vi.fn((signal) => {
      reconciliationSignal = signal;
      return reconciliation.promise;
    });
    const view = render(<UnlockPage status={{ databases: [] }} onUnlocked={onUnlocked} />);

    await user.type(screen.getByLabelText("Database password"), "StrongDatabase123");
    await user.type(screen.getByLabelText("Confirm password"), "StrongDatabase123");
    await user.click(screen.getByRole("button", { name: "Create encrypted database" }));
    await waitFor(() => expect(onUnlocked).toHaveBeenCalledOnce());
    expect(reconciliationSignal.aborted).toBe(false);

    view.unmount();
    expect(reconciliationSignal.aborted).toBe(true);
    reconciliation.resolve();
    await Promise.resolve();
  });

  it("requires a name when creating another encrypted database", async () => {
    const user = userEvent.setup();
    apiPost.mockResolvedValueOnce({});
    render(<UnlockPage status={status} onUnlocked={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "New Database" }));
    const name = screen.getByLabelText("Database name");
    expect(name).toBeRequired();
    expect(name).toHaveAttribute("placeholder", "Project name");
    await user.type(name, "Second");
    await user.type(screen.getByLabelText("Database password"), "StrongDatabase123");
    await user.type(screen.getByLabelText("Confirm password"), "StrongDatabase123");
    await user.click(screen.getByRole("button", { name: "Create encrypted database" }));

    await waitFor(() =>
      expect(apiPost).toHaveBeenCalledWith(
        "/api/unlock/setup",
        { database_name: "Second", password: "StrongDatabase123", confirm_password: "StrongDatabase123" },
        { signal: expect.any(AbortSignal) },
      ),
    );
  });
});
