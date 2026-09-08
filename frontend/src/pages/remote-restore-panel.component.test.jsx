import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../lib/api";
import { RemoteRestorePanel } from "./remote-restore-panel";

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
  const runLifecycleMutation = async (_name, execute) => {
    await execute(new AbortController().signal);
    await onUnlocked();
  };
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
  const view = render(<RemoteRestorePanel runLifecycleMutation={runLifecycleMutation} />);

  await user.type(screen.getByLabelText("Backup service URL"), "https://backup-a.example.com");
  await user.type(screen.getByLabelText("Service token"), "token-a");
  await user.click(screen.getByRole("button", { name: "Connect and list backups" }));
  const staleSignal = apiPost.mock.calls[0][2].signal;
  await user.clear(screen.getByLabelText("Backup service URL"));
  await user.type(screen.getByLabelText("Backup service URL"), "https://backup-b.example.com");
  await user.clear(screen.getByLabelText("Service token"));
  await user.type(screen.getByLabelText("Service token"), "token-b");
  await user.click(screen.getByRole("button", { name: "Connect and list backups" }));

  expect(staleSignal.aborted).toBe(true);
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

it("aborts a pending backup listing when the panel unmounts", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  apiPost.mockReturnValueOnce(pending.promise);
  const view = render(<RemoteRestorePanel runLifecycleMutation={vi.fn()} />);

  await user.type(screen.getByLabelText("Backup service URL"), "https://backup.example.com");
  await user.type(screen.getByLabelText("Service token"), "token");
  await user.click(screen.getByRole("button", { name: "Connect and list backups" }));
  const signal = apiPost.mock.calls[0][2].signal;
  view.unmount();

  expect(signal.aborted).toBe(true);
  pending.resolve({ items: [] });
  await Promise.resolve();
});

it("retains restore credentials and reports a failed lifecycle reconciliation", async () => {
  const user = userEvent.setup();
  apiPost.mockImplementation((_requestPath, body) => {
    if (body?.stream_id) {
      return Promise.resolve({ items: [{ backups: [{ id: "backup-1", filename: "backup.aipdb", created_at: "2026-01-01T00:00:00Z" }] }] });
    }
    return Promise.resolve({ items: [{ id: "stream-1", database_name: "Restored" }] });
  });
  const runLifecycleMutation = async (_name, execute) => {
    await execute(new AbortController().signal);
    throw new Error("Status reconciliation failed");
  };
  render(<RemoteRestorePanel runLifecycleMutation={runLifecycleMutation} />);

  await user.type(screen.getByLabelText("Backup service URL"), "https://backup.example.com");
  await user.type(screen.getByLabelText("Service token"), "service-token");
  await user.click(screen.getByRole("button", { name: "Connect and list backups" }));
  expect(await screen.findByRole("option", { name: /Restored/ })).toBeVisible();
  await user.type(screen.getByLabelText("Backup database password"), "StrongPassword123");
  await user.click(screen.getByRole("button", { name: "Restore encrypted database" }));

  expect(await screen.findByText("Status reconciliation failed")).toBeVisible();
  expect(screen.getByLabelText("Service token")).toHaveValue("service-token");
  expect(screen.getByLabelText("Backup database password")).toHaveValue("StrongPassword123");
  expect(screen.getByRole("button", { name: "Restore encrypted database" })).toBeEnabled();
});

it("locks remote credentials while restore outcome is authoritative", async () => {
  const user = userEvent.setup();
  const restore = deferred();
  apiPost.mockImplementation((_requestPath, body) => {
    if (body?.backup_id) return restore.promise;
    if (body?.stream_id) {
      return Promise.resolve({ items: [{ backups: [{ id: "backup-1", filename: "backup.aipdb", created_at: "2026-01-01T00:00:00Z" }] }] });
    }
    return Promise.resolve({ items: [{ id: "stream-1", database_name: "Restored" }] });
  });
  const runLifecycleMutation = async (_name, execute) => execute(new AbortController().signal);
  render(<RemoteRestorePanel runLifecycleMutation={runLifecycleMutation} />);

  await user.type(screen.getByLabelText("Backup service URL"), "https://backup.example.com");
  await user.type(screen.getByLabelText("Service token"), "service-token");
  await user.click(screen.getByRole("button", { name: "Connect and list backups" }));
  expect(await screen.findByRole("option", { name: /Restored/ })).toBeVisible();
  await user.type(screen.getByLabelText("Backup database password"), "StrongPassword123");
  await user.click(screen.getByRole("button", { name: "Restore encrypted database" }));
  await waitFor(() => expect(screen.getByLabelText("Service token")).toBeDisabled());
  expect(screen.getByLabelText("New local database name")).toBeDisabled();
  expect(screen.getByLabelText("Backup database password")).toBeDisabled();

  restore.reject(new Error("Restore failed after dispatch"));

  expect(await screen.findByText("Restore failed after dispatch")).toBeVisible();
  expect(screen.getByLabelText("Service token")).toHaveValue("service-token");
});

it("loads the selected stream and lets the operator choose a version and local name", async () => {
  const user = userEvent.setup();
  apiPost.mockImplementation((_requestPath, body) => {
    if (body?.stream_id === "stream-a") {
      return Promise.resolve({ items: [{ backups: [{ id: "backup-a", filename: "a.aipdb", created_at: "2026-01-01T00:00:00Z" }] }] });
    }
    if (body?.stream_id === "stream-b") {
      return Promise.resolve({
        items: [
          {
            backups: [
              { id: "backup-b1", filename: "b1.aipdb", created_at: "2026-01-02T00:00:00Z" },
              { id: "backup-b2", filename: "b2.aipdb", created_at: "2026-01-03T00:00:00Z" },
            ],
          },
        ],
      });
    }
    return Promise.resolve({
      items: [
        { id: "stream-a", database_name: "Database A" },
        { id: "stream-b", database_name: "Database B" },
      ],
    });
  });
  render(<RemoteRestorePanel runLifecycleMutation={vi.fn()} />);

  await user.type(screen.getByLabelText("Backup service URL"), "https://backup.example.com");
  await user.type(screen.getByLabelText("Service token"), "service-token");
  await user.click(screen.getByRole("button", { name: "Connect and list backups" }));
  await user.selectOptions(await screen.findByLabelText("Database stream"), "stream-b");
  await waitFor(() => expect(screen.getByLabelText("Backup version")).toHaveValue("backup-b1"));
  await user.selectOptions(screen.getByLabelText("Backup version"), "backup-b2");
  await user.clear(screen.getByLabelText("New local database name"));
  await user.type(screen.getByLabelText("New local database name"), "Restored B");

  expect(screen.getByLabelText("Backup version")).toHaveValue("backup-b2");
  expect(screen.getByLabelText("New local database name")).toHaveValue("Restored B");
  expect(apiPost).toHaveBeenCalledWith("/api/backup/remote/list", expect.objectContaining({ stream_id: "stream-b" }), {
    signal: expect.any(AbortSignal),
  });
});
