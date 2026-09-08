import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiDownload, apiPost } from "../../../lib/api";
import { BackupRestoreDialog } from "./backup-restore-dialog";
import { PostgresConnectorOperationsTemplate } from "./operations";
import { ProvisionUserDialog } from "./provision-user-dialog";

vi.mock("../../../lib/api", () => ({
  apiDownload: vi.fn(),
  apiPost: vi.fn(),
  apiPostForm: vi.fn(),
}));

beforeEach(() => {
  apiDownload.mockReset();
  apiPost.mockReset().mockResolvedValue({
    request_id: 1,
    status: "completed",
    output: { rows: [{ table_schema: "public", table_name: "users", columns: ["id"] }] },
  });
});

describe("Postgres operation dialogs", () => {
  it("routes connector operations to the matching dialog", () => {
    const onChange = vi.fn();
    const { rerender } = render(<PostgresConnectorOperationsTemplate value={operation("provision-user")} onChange={onChange} />);
    expect(screen.getByRole("dialog", { name: "Main DB managed DB user" })).toBeInTheDocument();

    rerender(<PostgresConnectorOperationsTemplate value={operation("backup-restore")} onChange={onChange} />);
    expect(screen.getByRole("dialog", { name: "Main DB backup / restore" })).toBeInTheDocument();

    rerender(<PostgresConnectorOperationsTemplate value={operation("unknown")} onChange={onChange} />);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("cannot close while managed credential provisioning is running", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    apiPost.mockImplementation((path) =>
      path.endsWith("/provision") ? new Promise(() => {}) : Promise.resolve({ request_id: 1, status: "completed", output: { rows: [] } }),
    );
    render(<ProvisionUserDialog value={operation("provision-user")} onClose={onClose} />);

    await user.type(screen.getByLabelText("Role name"), "app_reader");
    await user.click(screen.getByRole("button", { name: "Create user" }));
    expect(await screen.findByRole("button", { name: "Creating user" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Close dialog" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Close" })).toBeDisabled();

    await user.keyboard("{Escape}");
    expect(onClose).not.toHaveBeenCalled();
  });

  it("cannot close while a backup download is running", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    apiDownload.mockImplementation(() => new Promise(() => {}));
    render(<BackupRestoreDialog value={operation("backup-restore")} onClose={onClose} />);

    await user.click(screen.getByRole("button", { name: "Download SQL dump" }));
    expect(await screen.findByRole("button", { name: "Working" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Close dialog" })).toBeDisabled();

    await user.keyboard("{Escape}");
    expect(onClose).not.toHaveBeenCalled();
  });
});

function operation(type) {
  return {
    open: true,
    connector_kind: "postgres",
    type,
    target: { id: 1, name: "Main DB", config: { database: "app" } },
    profile: { id: 10, ref: "postgres:1:10" },
  };
}
