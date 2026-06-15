import { useEffect, useState } from "react";
import { ExternalLink, LockKeyhole, Trash2, Upload } from "lucide-react";
import { apiPost, apiPostForm } from "../lib/api";
import { Button } from "../components/ui/button";
import { Dialog } from "../components/ui/dialog";
import { Input } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { isValidDatabasePassword } from "../lib/password";

export function UnlockPage({ status, onUnlocked }) {
  const databases = status?.databases || [];
  const firstDatabaseID = status?.database_id || databases[0]?.id || "default";
  const [selectedDatabaseID, setSelectedDatabaseID] = useState(firstDatabaseID);
  const selectedDatabase = databases.find((database) => database.id === selectedDatabaseID) || databases[0] || null;
  const hasDatabase = Boolean(selectedDatabase);
  const selectedUnsupported = selectedDatabase?.state === "unsupported_plaintext";
  const [migrationRequiredIDs, setMigrationRequiredIDs] = useState({});
  const selectedMigrationRequired = Boolean(selectedDatabase && migrationRequiredIDs[selectedDatabase.id]);
  const [activeTab, setActiveTab] = useState(hasDatabase ? "unlock" : "create");
  const [createForm, setCreateForm] = useState({ database_name: "", password: "", confirm_password: "" });
  const [unlockForm, setUnlockForm] = useState({ password: "" });
  const [deleteDialog, setDeleteDialog] = useState({ open: false, password: "", state: "idle", error: null });
  const [toast, setToast] = useState("");
  const [importForm, setImportForm] = useState({ file: null, database_password: "" });
  const [createState, setCreateState] = useState({ state: "idle", error: null });
  const [unlockState, setUnlockState] = useState({ state: "idle", error: null });
  const [importState, setImportState] = useState({ state: "idle", error: null });

  useEffect(() => {
    const nextID = status?.database_id || databases[0]?.id || "default";
    setSelectedDatabaseID(nextID);
  }, [status?.database_id, databases.length]);

  useEffect(() => {
    if (!selectedDatabase) {
      setActiveTab("create");
      return;
    }
    setActiveTab("unlock");
  }, [selectedDatabase?.id, selectedDatabase?.state]);

  const tabs = [
    ...(hasDatabase ? [["unlock", "Unlock Database"]] : []),
    ["create", hasDatabase ? "New Database" : "Create Database"],
    ["import", "Import Database"],
  ];
  const createPasswordValid = isValidDatabasePassword(createForm.password);

  function showToast(message) {
    setToast(message);
    window.setTimeout(() => setToast(""), 2400);
  }

  async function createDatabase(event) {
    event.preventDefault();
    setCreateState({ state: "saving", error: null });
    try {
      await apiPost("/api/unlock/setup", {
        password: createForm.password,
        confirm_password: createForm.confirm_password,
        database_name: createForm.database_name,
      });
      await onUnlocked();
    } catch (error) {
      setCreateState({ state: "error", error: error.message });
    }
  }

  async function unlockDatabase(event) {
    event.preventDefault();
    setUnlockState({ state: "unlocking", error: null });
    try {
      await apiPost("/api/unlock", { database_id: selectedDatabase?.id, password: unlockForm.password });
      await onUnlocked();
    } catch (error) {
      if (isMigrationRequiredError(error) && selectedDatabase?.id) {
        setMigrationRequiredIDs((current) => ({ ...current, [selectedDatabase.id]: true }));
      }
      setUnlockState({ state: "error", error: error.message });
    }
  }

  function openDeleteDialog() {
    setDeleteDialog({ open: true, password: "", state: "idle", error: null });
  }

  function closeDeleteDialog() {
    setDeleteDialog((current) => ({ ...current, open: false }));
  }

  async function deleteLockedDatabase(event) {
    event.preventDefault();
    if (!selectedDatabase) return;
    setDeleteDialog((current) => ({ ...current, state: "deleting", error: null }));
    try {
      await apiPost("/api/databases/delete-locked", {
        database_id: selectedDatabase.id,
        current_password: deleteDialog.password,
      });
      setDeleteDialog({ open: false, password: "", state: "idle", error: null });
      setUnlockForm({ password: "" });
      setMigrationRequiredIDs((current) => {
        const next = { ...current };
        delete next[selectedDatabase.id];
        return next;
      });
      await onUnlocked();
      showToast("Local database deleted.");
    } catch (error) {
      setDeleteDialog((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  async function importDatabase(event) {
    event.preventDefault();
    if (!importForm.file) {
      setImportState({ state: "error", error: "Database file is required" });
      return;
    }
    setImportState({ state: "importing", error: null });
    try {
      const formData = new FormData();
      formData.set("sqlite", importForm.file, importForm.file.name);
      formData.set("database_password", importForm.database_password);
      formData.set("database_name", createForm.database_name);
      await apiPostForm("/api/backup/import", formData);
      await onUnlocked();
    } catch (error) {
      setImportState({ state: "error", error: error.message });
    }
  }

  return (
    <UnlockShell title={hasDatabase ? "Select database" : "Database setup"}>
      {toast ? <Toast message={toast} /> : null}
      {databases.length > 0 ? (
        <div className="grid gap-2">
          <label className="text-sm font-semibold text-stone-800">Database</label>
          <select
            className="h-10 rounded-md border border-stone-300 bg-white px-3 text-sm outline-none focus:border-emerald-800"
            value={selectedDatabase?.id || selectedDatabaseID}
            onChange={(event) => setSelectedDatabaseID(event.target.value)}
          >
            {databases.map((database) => (
              <option key={database.id} value={database.id}>
                {database.name} {database.state === "unsupported_plaintext" ? "(unsupported plaintext)" : ""}
              </option>
            ))}
          </select>
        </div>
      ) : null}
      {status?.state === "session_required" ? (
        <Notice tone="warn">Your browser session is missing or expired. Enter the database password to continue.</Notice>
      ) : null}
      {selectedUnsupported ? (
        <Notice tone="bad">This file is a plaintext SQLite database. AIPermission only supports SQLCipher-encrypted .aipdb databases.</Notice>
      ) : null}
      {selectedMigrationRequired ? (
        <Notice tone="warn">
          This database uses the pre-0.2 schema. Open the local migration helper, migrate it into a new 0.2 database, then delete this old local copy when you no longer need it.
        </Notice>
      ) : null}
      <div className="grid rounded-md border border-stone-200 bg-stone-100 p-1" style={{ gridTemplateColumns: `repeat(${tabs.length}, minmax(0, 1fr))` }}>
        {tabs.map(([value, label]) => (
          <button
            key={value}
            type="button"
            className={`h-10 whitespace-nowrap rounded px-2 text-xs font-semibold transition sm:text-sm ${
              activeTab === value ? "bg-white text-emerald-950 shadow-sm" : "text-stone-500 hover:text-stone-900"
            }`}
            onClick={() => setActiveTab(value)}
          >
            {label}
          </button>
        ))}
      </div>

      {activeTab === "unlock" ? (
        <form className="grid gap-4" onSubmit={unlockDatabase}>
          <div className="grid gap-2">
            <label className="text-sm font-semibold text-stone-800">Database password</label>
            <Input
              type="password"
              value={unlockForm.password}
              onChange={(event) => setUnlockForm((current) => ({ ...current, password: event.target.value }))}
              autoFocus
              required
            />
          </div>
          {unlockState.state === "error" ? <Notice tone="bad">{unlockState.error}</Notice> : null}
          {selectedMigrationRequired ? (
            <div className="grid gap-2 sm:grid-cols-2">
              <Button type="button" asChild>
                <a href="http://localhost:3211" target="_blank" rel="noreferrer">
                  <ExternalLink className="h-4 w-4" />
                  Open migration helper
                </a>
              </Button>
              <Button type="button" variant="danger" onClick={openDeleteDialog}>
                <Trash2 className="h-4 w-4" />
                Delete old local copy
              </Button>
            </div>
          ) : null}
          <Button type="submit" disabled={unlockState.state === "unlocking" || selectedUnsupported || selectedMigrationRequired}>
            <LockKeyhole className="h-4 w-4" />
            {unlockState.state === "unlocking" ? "Unlocking..." : "Unlock"}
          </Button>
          {!selectedMigrationRequired && selectedDatabase ? (
            <button type="button" className="justify-self-center text-sm font-semibold text-red-700 hover:text-red-800" onClick={openDeleteDialog}>
              Delete this local database
            </button>
          ) : null}
        </form>
      ) : null}

      {activeTab === "create" ? (
        <form className="grid gap-4" onSubmit={createDatabase}>
          <div className="grid gap-2">
            <label className="text-sm font-semibold text-stone-800">Database name</label>
            <Input
              type="text"
              value={createForm.database_name}
              onChange={(event) => setCreateForm((current) => ({ ...current, database_name: event.target.value }))}
              placeholder={hasDatabase ? "Project name" : "Default"}
              required={hasDatabase}
            />
          </div>
          <div className="grid gap-2">
            <label className="text-sm font-semibold text-stone-800">Database password</label>
            <Input
              type="password"
              value={createForm.password}
              onChange={(event) => setCreateForm((current) => ({ ...current, password: event.target.value }))}
              minLength={14}
              autoFocus={!hasDatabase}
              required
            />
          </div>
          <div className="grid gap-2">
            <label className="text-sm font-semibold text-stone-800">Confirm password</label>
            <Input
              type="password"
              value={createForm.confirm_password}
              onChange={(event) => setCreateForm((current) => ({ ...current, confirm_password: event.target.value }))}
              minLength={14}
              required
            />
          </div>
          <Notice>Use at least 14 characters with uppercase letters, lowercase letters, and numbers. This password cannot be recovered.</Notice>
          {createState.state === "error" ? <Notice tone="bad">{createState.error}</Notice> : null}
          <Button type="submit" disabled={createState.state === "saving" || !createPasswordValid || createForm.password !== createForm.confirm_password}>
            <LockKeyhole className="h-4 w-4" />
            {createState.state === "saving" ? "Working..." : "Create encrypted database"}
          </Button>
        </form>
      ) : null}

      {activeTab === "import" ? (
        <form className="grid gap-4" onSubmit={importDatabase}>
          <div>
            <h2 className="text-sm font-semibold text-stone-900">Import encrypted database</h2>
            <p className="mt-1 text-sm text-stone-500">Choose an exported .aipdb or SQLCipher .db file, then enter that database password. Imports always create a new named database.</p>
          </div>
          <div className="grid gap-2">
            <label className="text-sm font-semibold text-stone-800">Database name</label>
            <Input
              type="text"
              value={createForm.database_name}
              onChange={(event) => setCreateForm((current) => ({ ...current, database_name: event.target.value }))}
              placeholder="Restored project"
              required
            />
          </div>
          <div className="grid gap-2">
            <label className="text-sm font-semibold text-stone-800">Database file</label>
            <Input
              type="file"
              accept=".aipdb,.db,application/octet-stream"
              onChange={(event) => setImportForm((current) => ({ ...current, file: event.target.files?.[0] || null }))}
              required
            />
          </div>
          <div className="grid gap-2">
            <label className="text-sm font-semibold text-stone-800">Database password</label>
            <Input
              type="password"
              value={importForm.database_password}
              onChange={(event) => setImportForm((current) => ({ ...current, database_password: event.target.value }))}
              required
            />
          </div>
          {importState.state === "error" ? <Notice tone="bad">{importState.error}</Notice> : null}
          <Button type="submit" variant="outline" disabled={importState.state === "importing"}>
            <Upload className="h-4 w-4" />
            {importState.state === "importing" ? "Importing..." : "Import database"}
          </Button>
        </form>
      ) : null}
      <Dialog
        open={deleteDialog.open}
        title="Delete local database"
        description={selectedDatabase ? `Delete ${selectedDatabase.name} from this local gateway.` : ""}
        onClose={closeDeleteDialog}
        closeDisabled={deleteDialog.state === "deleting"}
        closeOnOverlay={false}
        size="md"
      >
        <form className="grid gap-4" onSubmit={deleteLockedDatabase}>
          <Notice tone="bad">
            This only removes the local database file from this gateway. If you have not migrated or backed it up, its local configuration will be lost.
          </Notice>
          <div className="rounded-md border border-stone-200 bg-stone-50 p-3 text-sm text-stone-700">
            <span className="font-semibold text-stone-900">Database:</span> {selectedDatabase?.name || "Unknown"}
          </div>
          <div className="grid gap-2">
            <label className="text-sm font-semibold text-stone-800">Database password</label>
            <Input
              type="password"
              value={deleteDialog.password}
              onChange={(event) => setDeleteDialog((current) => ({ ...current, password: event.target.value }))}
              autoFocus
              required
            />
          </div>
          {deleteDialog.state === "error" ? <Notice tone="bad">{deleteDialog.error}</Notice> : null}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={closeDeleteDialog} disabled={deleteDialog.state === "deleting"}>
              Cancel
            </Button>
            <Button type="submit" variant="danger" disabled={deleteDialog.state === "deleting" || !deleteDialog.password}>
              <Trash2 className="h-4 w-4" />
              {deleteDialog.state === "deleting" ? "Deleting..." : "Delete permanently"}
            </Button>
          </div>
        </form>
      </Dialog>
    </UnlockShell>
  );
}

function isMigrationRequiredError(error) {
  return error?.status === 409 && /pre-0\.2|non-baseline schema|migration helper/i.test(error?.message || "");
}

function Toast({ message }) {
  return (
    <div role="status" aria-live="polite" className="fixed right-5 top-5 z-[80] rounded-md border border-stone-700 bg-stone-950 px-4 py-3 text-sm font-semibold text-white shadow-xl">
      {message}
    </div>
  );
}

export function UnlockShell({ title, children }) {
  return (
    <main className="grid min-h-screen place-items-center bg-stone-100 p-5 text-stone-950">
      <section className="grid w-full max-w-2xl gap-5 rounded-lg border border-stone-200 bg-white p-6 shadow-xl">
        <div className="flex items-center gap-3">
          <img src="/icon.svg" alt="" className="h-10 w-10 rounded-lg" />
          <div>
            <h1 className="text-lg font-semibold">aipermission</h1>
            <p className="text-sm text-stone-500">{title}</p>
          </div>
        </div>
        <Notice tone="warn">
          Local-only gateway. Keep Docker ports bound to <span className="font-mono">127.0.0.1</span>; do not expose this UI or API on LAN or the public internet.
        </Notice>
        {children}
      </section>
    </main>
  );
}
