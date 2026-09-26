import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiPost } from "../../lib/api";
import { DatabaseSettingsPanel } from "./database-settings-panel";

vi.mock("../../lib/api", () => ({ apiPost: vi.fn() }));

describe("database settings form contracts", () => {
  beforeEach(() => {
    vi.mocked(apiPost).mockReset();
  });

  it("requires the exact database name before opening destructive confirmation", async () => {
    const user = userEvent.setup();
    render(<DatabaseSettingsPanel databaseName="My database" />);
    expect(screen.getByRole("button", { name: "Delete database" })).toBeDisabled();
    await user.type(screen.getByLabelText("Confirm database name"), "My database");
    await user.click(screen.getByRole("button", { name: "Delete database" }));
    const dialog = screen.getByRole("dialog", { name: "Delete database" });
    expect(within(dialog).getByRole("button", { name: "Delete permanently" })).toBeDisabled();
    expect(apiPost).not.toHaveBeenCalled();
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("submits the typed name and password and keeps failed deletion recoverable", async () => {
    const user = userEvent.setup();
    vi.mocked(apiPost).mockRejectedValue(new Error("Deletion refused"));
    render(<DatabaseSettingsPanel databaseName="My database" />);
    await user.type(screen.getByLabelText("Confirm database name"), "My database");
    await user.click(screen.getByRole("button", { name: "Delete database" }));
    const dialog = screen.getByRole("dialog", { name: "Delete database" });
    await user.type(within(dialog).getByLabelText("Current database password"), "MyPassword123");
    await user.click(within(dialog).getByRole("button", { name: "Delete permanently" }));
    expect(apiPost).toHaveBeenCalledWith("/api/databases/delete", { confirm_name: "My database", current_password: "MyPassword123" });
    expect(await within(dialog).findByText("Deletion refused")).toBeVisible();
    expect(within(dialog).getByRole("button", { name: "Delete permanently" })).toBeEnabled();
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await user.click(screen.getByRole("button", { name: "Delete database" }));
    expect(within(screen.getByRole("dialog")).getByLabelText("Current database password")).toHaveValue("");
  });

  it("updates the rename form when the active database name changes", async () => {
    const { rerender } = render(<DatabaseSettingsPanel databaseName="First" />);
    fireEvent.change(screen.getByLabelText("Database name"), { target: { value: "Unsaved" } });
    rerender(<DatabaseSettingsPanel databaseName="Second" />);
    await waitFor(() => expect(screen.getByLabelText("Database name")).toHaveValue("Second"));
    expect(screen.getByRole("button", { name: "Rename database" })).toBeDisabled();
  });
});
