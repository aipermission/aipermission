import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../lib/api";
import { RemoteRestorePanel } from "./unlock";

vi.mock("../lib/api", () => ({ apiPost: vi.fn(), apiPostForm: vi.fn() }));

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

beforeEach(() => apiPost.mockReset());

it("ignores stale service responses and restores only with the current credential fingerprint", async () => {
  const user = userEvent.setup();
  const stale = deferred();
  const restore = deferred();
  const onUnlocked = vi.fn();
  const requests = [];
  apiPost.mockImplementation((requestPath, body) => {
    if (!body) return Promise.resolve({});
    requests.push([requestPath, body]);
    if (body.token === "token-a") return stale.promise;
    if (body.backup_id === "backup-b") return restore.promise;
    if (body.stream_id === "stream-b") {
      return Promise.resolve({
        items: [{ backups: [{ id: "backup-b", filename: "b.aipdb", created_at: "2026-01-01T00:00:00Z", size_bytes: 10 }] }],
      });
    }
    return Promise.resolve({ items: [{ id: "stream-b", database_name: "Database B" }] });
  });
  const view = render(<RemoteRestorePanel onUnlocked={onUnlocked} />);

  await user.type(screen.getByLabelText("Backup service URL"), "https://backup-a.example.com");
  await user.type(screen.getByLabelText("Service token"), "token-a");
  await user.click(screen.getByRole("button", { name: "Connect and list backups" }));
  await user.clear(screen.getByLabelText("Backup service URL"));
  await user.type(screen.getByLabelText("Backup service URL"), "https://backup-b.example.com");
  await user.clear(screen.getByLabelText("Service token"));
  await user.type(screen.getByLabelText("Service token"), "token-b");
  await user.click(screen.getByRole("button", { name: "Connect and list backups" }));

  expect(await screen.findByRole("option", { name: /Database B/ })).toBeVisible();
  stale.resolve({ items: [{ id: "stream-a", database_name: "Database A" }] });
  await waitFor(() => expect(screen.queryByRole("option", { name: /Database A/ })).not.toBeInTheDocument());

  await user.type(screen.getByLabelText("Backup database password"), "StrongPassword123");
  await user.click(screen.getByRole("button", { name: "Restore encrypted database" }));
  await waitFor(() => expect(requests.at(-1)?.[0]).toBe("/api/backup/remote/restore"));
  view.unmount();
  restore.resolve({});
  await waitFor(() => expect(onUnlocked).toHaveBeenCalledOnce());
  expect(requests.at(-1)).toEqual([
    "/api/backup/remote/restore",
    {
      base_url: "https://backup-b.example.com",
      token: "token-b",
      stream_id: "stream-b",
      backup_id: "backup-b",
      database_name: "Database B",
      database_password: "StrongPassword123",
    },
  ]);
});
