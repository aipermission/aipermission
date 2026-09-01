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
    expect(apiPost).toHaveBeenLastCalledWith("/api/unlock", { database_id: "db-1", password: "CorrectPassword123" });
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
    expect(apiPost).toHaveBeenLastCalledWith("/api/databases/delete-locked", {
      database_id: "db-1",
      current_password: "OldPassword123",
    });
  });

  it("validates creation locally and reports an import without a selected file", async () => {
    const user = userEvent.setup();
    render(<UnlockPage status={{ databases: [] }} onUnlocked={vi.fn()} />);

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
});
